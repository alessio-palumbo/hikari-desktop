package backend

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

const (
	maxScenes             = 128
	maxSceneDocumentBytes = 32 << 20
	maxSceneDevices       = 64
	maxSceneCells         = 40000
)

// Scene stores physical library snapshots, not UI-oriented previews or selectors.
// Serial membership is explicit; labels are only display fallbacks.
type Scene struct {
	ID        string                   `json:"id"`
	Name      string                   `json:"name"`
	SavedAtMS int64                    `json:"savedAtMs"`
	Snapshot  lifxdevice.StateSnapshot `json:"snapshot"`
	Labels    map[string]string        `json:"labels"`
}

type SceneTarget struct {
	Serial     string     `json:"serial"`
	Label      string     `json:"label"`
	On         bool       `json:"on"`
	Brightness float64    `json:"brightness"`
	Colors     []HSLColor `json:"colors"`
}

type SceneSummary struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	SavedAtMS int64         `json:"savedAtMs"`
	Targets   []SceneTarget `json:"targets"`
}

func (s Scene) Summary() SceneSummary {
	result := SceneSummary{ID: s.ID, Name: s.Name, SavedAtMS: s.SavedAtMS, Targets: []SceneTarget{}}
	for _, d := range s.Snapshot.Devices {
		serial := d.Serial.String()
		target := SceneTarget{Serial: serial, Label: nameOrUnknown(s.Labels[serial], serial), On: d.PoweredOn}
		colors := []HSLColor{mapLifxColor(d.Color, DeviceCapability{HasColor: true})}
		if len(d.Zones) > 0 {
			colors = mapLifxColors(d.Zones, DeviceCapability{HasColor: true})
		}
		if len(d.MatrixChains) > 0 {
			colors = nil
			for _, chain := range d.MatrixChains {
				colors = append(colors, mapLifxColors(chain, DeviceCapability{HasColor: true})...)
			}
		}
		unique := []HSLColor{}
		seen := make(map[HSLColor]bool)
		for _, color := range colors {
			target.Brightness += color.L / float64(len(colors))
			// Swatches describe color, not repeated brightness levels of that color.
			if color.L > 0 {
				color.L = 1
			}
			if !seen[color] {
				unique = append(unique, color)
				seen[color] = true
			}
		}
		if len(unique) <= 5 {
			target.Colors = unique
		} else {
			for i := 0; i < 5; i++ {
				target.Colors = append(target.Colors, unique[i*(len(unique)-1)/4])
			}
		}
		result.Targets = append(result.Targets, target)
	}
	return result
}

type SceneStore interface {
	Load() ([]Scene, error)
	Save(Scene) (Scene, error)
	Rename(id, name string) (Scene, error)
	Delete(id string) error
}

type sceneDocument struct {
	Version int     `json:"version"`
	Scenes  []Scene `json:"scenes"`
}

type fileSceneStore struct {
	mu        sync.Mutex
	path      string
	pathError error
}

func NewSceneStore() SceneStore {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return &fileSceneStore{pathError: errors.New("user config directory is unavailable")}
	}
	return &fileSceneStore{path: filepath.Join(dir, "hikari", "scenes.json")}
}

func (s *fileSceneStore) read() (sceneDocument, error) {
	empty := sceneDocument{Version: 1, Scenes: []Scene{}}
	if s.pathError != nil {
		return empty, s.pathError
	}
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("read scenes: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSceneDocumentBytes+1))
	if err != nil {
		return empty, err
	}
	if len(data) > maxSceneDocumentBytes {
		return empty, errors.New("scenes document is too large")
	}
	var document sceneDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return empty, fmt.Errorf("read scenes: %w", err)
	}
	if document.Version != 1 {
		return empty, errors.New("unsupported scenes version")
	}
	if len(document.Scenes) > maxScenes {
		return empty, errors.New("too many scenes")
	}
	seen := make(map[string]bool)
	for _, scene := range document.Scenes {
		if !validSceneID(scene.ID) || seen[scene.ID] {
			return empty, errors.New("invalid or duplicate scene ID")
		}
		seen[scene.ID] = true
		if err := validateScene(scene); err != nil {
			return empty, fmt.Errorf("scene %s: %w", scene.ID, err)
		}
	}
	if document.Scenes == nil {
		document.Scenes = []Scene{}
	}
	return document, nil
}

func (s *fileSceneStore) Load() ([]Scene, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.read()
	if err != nil {
		return nil, err
	}
	slices.SortFunc(document.Scenes, func(a, b Scene) int {
		if n := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return document.Scenes, nil
}

func (s *fileSceneStore) Save(scene Scene) (Scene, error) {
	scene.Name = strings.TrimSpace(scene.Name)
	if err := validateScene(scene); err != nil {
		return Scene{}, err
	}
	if scene.ID != "" && !validSceneID(scene.ID) {
		return Scene{}, errors.New("invalid scene ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.read()
	if err != nil {
		return Scene{}, err
	}
	if scene.ID == "" {
		if len(document.Scenes) >= maxScenes {
			return Scene{}, errors.New("too many scenes")
		}
		scene.ID = "scene-" + rand.Text()
		document.Scenes = append(document.Scenes, scene)
	} else {
		index := slices.IndexFunc(document.Scenes, func(item Scene) bool { return item.ID == scene.ID })
		if index < 0 {
			return Scene{}, errors.New("scene no longer exists")
		}
		document.Scenes[index] = scene
	}
	if err := s.write(document); err != nil {
		return Scene{}, err
	}
	return scene, nil
}

func (s *fileSceneStore) Rename(id, name string) (Scene, error) {
	name = strings.TrimSpace(name)
	if !validSceneID(id) {
		return Scene{}, errors.New("invalid scene ID")
	}
	if err := validateSceneName(name); err != nil {
		return Scene{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.read()
	if err != nil {
		return Scene{}, err
	}
	index := slices.IndexFunc(document.Scenes, func(item Scene) bool { return item.ID == id })
	if index < 0 {
		return Scene{}, errors.New("scene no longer exists")
	}
	document.Scenes[index].Name = name
	if err := s.write(document); err != nil {
		return Scene{}, err
	}
	return document.Scenes[index], nil
}

func (s *fileSceneStore) Delete(id string) error {
	if !validSceneID(id) {
		return errors.New("invalid scene ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.read()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(document.Scenes, func(item Scene) bool { return item.ID == id })
	if index < 0 {
		return errors.New("scene no longer exists")
	}
	document.Scenes = slices.Delete(document.Scenes, index, index+1)
	return s.write(document)
}

func (s *fileSceneStore) write(document sceneDocument) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > maxSceneDocumentBytes {
		return errors.New("scenes document is too large")
	}
	return writeFileAtomic(s.path, append(data, '\n'), 0o600)
}

func validSceneID(id string) bool {
	if !strings.HasPrefix(id, "scene-") || len(id) <= 6 || len(id) > 80 {
		return false
	}
	for _, c := range id[6:] {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}

func validateSceneName(name string) error {
	if strings.TrimSpace(name) == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 128 {
		return errors.New("scene name must contain 1..128 characters")
	}
	return nil
}

func validateScene(scene Scene) error {
	if err := validateSceneName(scene.Name); err != nil {
		return err
	}
	if len(scene.Snapshot.Devices) == 0 || len(scene.Snapshot.Devices) > maxSceneDevices {
		return errors.New("scene requires 1..64 lights")
	}
	seen := make(map[lifxdevice.Serial]bool)
	labels := make(map[string]bool)
	cells := 0
	for _, state := range scene.Snapshot.Devices {
		parsed, err := lifxdevice.SerialFromHex(state.Serial.String())
		if err != nil || parsed != state.Serial || parsed == (lifxdevice.Serial{}) || seen[parsed] {
			return errors.New("invalid or duplicate scene device serial")
		}
		seen[parsed] = true
		labels[parsed.String()] = true
		for _, value := range []float64{state.Color.Hue, state.Color.Saturation, state.Color.Brightness} {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return errors.New("invalid scene color")
			}
		}
		if state.Color.Hue < 0 || state.Color.Hue > 360 || state.Color.Saturation < 0 || state.Color.Saturation > 100 || state.Color.Brightness < 0 || state.Color.Brightness > 100 {
			return errors.New("invalid scene color range")
		}
		count := 0
		switch state.LightType {
		case lifxdevice.LightTypeSingleZone:
			if len(state.Zones) != 0 || len(state.MatrixChains) != 0 {
				return errors.New("invalid single-zone scene state")
			}
			count = 1
		case lifxdevice.LightTypeMultiZone:
			if len(state.Zones) == 0 || len(state.MatrixChains) != 0 {
				return errors.New("missing multizone scene state")
			}
			count = len(state.Zones)
		case lifxdevice.LightTypeMatrix:
			if state.MatrixWidth <= 0 || state.MatrixWidth > 255 || len(state.MatrixChains) == 0 || len(state.MatrixChains) > 64 || len(state.Zones) != 0 {
				return errors.New("invalid matrix scene geometry")
			}
			for _, chain := range state.MatrixChains {
				if len(chain) == 0 || len(chain)%state.MatrixWidth != 0 {
					return errors.New("incomplete matrix scene state")
				}
				if len(chain)/state.MatrixWidth > 255 || len(chain) > 64 && 64%state.MatrixWidth != 0 {
					return errors.New("matrix geometry is not supported by snapshot restore")
				}
				count += len(chain)
			}
		default:
			return errors.New("unsupported scene light type")
		}
		if count > maxEffectPreviewCells || count > maxSceneCells-cells {
			return errors.New("scene state is too large")
		}
		cells += count
	}
	for serial, label := range scene.Labels {
		if !labels[serial] || !utf8.ValidString(label) || utf8.RuneCountInString(label) > 128 {
			return errors.New("invalid scene device label")
		}
	}
	return nil
}
