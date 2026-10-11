package backend

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
)

type CaptureSceneRequest struct {
	ID      string   `json:"id,omitempty"`
	Name    string   `json:"name"`
	Serials []string `json:"serials"`
}

type RenameSceneRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EditSceneRequest struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Serials []string `json:"serials"`
}

// Edit preserves retained snapshots and only observes newly added lights.
func (s *SceneService) Edit(ctx context.Context, req EditSceneRequest) (SceneSummary, error) {
	if err := ctx.Err(); err != nil {
		return SceneSummary{}, err
	}
	previous, err := s.find(req.ID)
	if err != nil {
		return SceneSummary{}, err
	}
	if err := validateSceneName(strings.TrimSpace(req.Name)); err != nil {
		return SceneSummary{}, err
	}
	serials, err := sceneSerials(req.Serials)
	if err != nil {
		return SceneSummary{}, err
	}
	saved := previous.Snapshot
	saved.Devices = nil
	labels := make(map[string]string)
	var added []string
	for _, serial := range serials {
		found := false
		for _, state := range previous.Snapshot.Devices {
			if state.Serial == serial {
				saved.Devices = append(saved.Devices, state)
				labels[serial.String()] = previous.Labels[serial.String()]
				found = true
				break
			}
		}
		if !found {
			added = append(added, serial.String())
		}
	}
	if len(added) > 0 {
		transport, ok := s.transport.(SceneTransport)
		if !ok {
			return SceneSummary{}, errors.New("scene capture is unavailable for this transport")
		}
		captured, names, err := transport.CaptureSceneState(ctx, added)
		if err != nil {
			return SceneSummary{}, err
		}
		if len(captured.Devices) != len(added) {
			return SceneSummary{}, errors.New("incomplete scene capture")
		}
		for _, serial := range added {
			found := false
			for _, state := range captured.Devices {
				if state.Serial.String() == serial {
					saved.Devices = append(saved.Devices, state)
					labels[serial] = names[serial]
					found = true
					break
				}
			}
			if !found {
				return SceneSummary{}, errors.New("incomplete scene capture")
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return SceneSummary{}, err
	}
	previous.Name, previous.Snapshot, previous.Labels = req.Name, saved, labels
	scene, err := s.store.Save(previous)
	if err != nil {
		return SceneSummary{}, err
	}
	return scene.Summary(), nil
}

type SceneRecallFailure struct {
	Serial              string `json:"serial"`
	Error               string `json:"error"`
	StateMayHaveChanged bool   `json:"stateMayHaveChanged"`
}

// Devices are the successfully sent target states, not acknowledgements.
type SceneRecallResult struct {
	Devices  []Device             `json:"devices"`
	Failures []SceneRecallFailure `json:"failures"`
}

type SceneTransport interface {
	CaptureSceneState(context.Context, []string) (lifxdevice.StateSnapshot, map[string]string, error)
	RecallSceneState(context.Context, lifxdevice.StateSnapshot) (SceneRecallResult, error)
}

type SceneService struct {
	transport DeviceTransport
	store     SceneStore
}

func NewSceneService(transport DeviceTransport, store SceneStore) *SceneService {
	return &SceneService{transport: transport, store: store}
}

func (s *SceneService) List() ([]SceneSummary, error) {
	entries, err := s.store.Load()
	if err != nil {
		return nil, err
	}
	result := make([]SceneSummary, 0, len(entries))
	for _, scene := range entries {
		result = append(result, scene.Summary())
	}
	return result, nil
}

func (s *SceneService) find(id string) (Scene, error) {
	if !validSceneID(id) {
		return Scene{}, errors.New("invalid scene ID")
	}
	entries, err := s.store.Load()
	if err != nil {
		return Scene{}, err
	}
	for _, scene := range entries {
		if scene.ID == id {
			return scene, nil
		}
	}
	return Scene{}, errors.New("scene no longer exists")
}

func (s *SceneService) Capture(ctx context.Context, req CaptureSceneRequest) (SceneSummary, error) {
	if err := ctx.Err(); err != nil {
		return SceneSummary{}, err
	}
	if req.ID != "" {
		previous, err := s.find(req.ID)
		if err != nil {
			return SceneSummary{}, err
		}
		// Recapture the saved membership, never whichever group is selected now.
		req.Name, req.Serials = previous.Name, nil
		for _, d := range previous.Snapshot.Devices {
			req.Serials = append(req.Serials, d.Serial.String())
		}
	}
	if err := validateSceneName(strings.TrimSpace(req.Name)); err != nil {
		return SceneSummary{}, err
	}
	transport, ok := s.transport.(SceneTransport)
	if !ok {
		return SceneSummary{}, errors.New("scene capture is unavailable for this transport")
	}
	state, labels, err := transport.CaptureSceneState(ctx, req.Serials)
	if err != nil {
		return SceneSummary{}, err
	}
	if err := ctx.Err(); err != nil {
		return SceneSummary{}, err
	}
	scene, err := s.store.Save(Scene{ID: req.ID, Name: req.Name, Snapshot: state, Labels: labels, SavedAtMS: time.Now().UnixMilli()})
	if err != nil {
		return SceneSummary{}, err
	}
	return scene.Summary(), nil
}

func (s *SceneService) Rename(req RenameSceneRequest) (SceneSummary, error) {
	scene, err := s.store.Rename(req.ID, req.Name)
	if err != nil {
		return SceneSummary{}, err
	}
	return scene.Summary(), nil
}

func (s *SceneService) Delete(id string) error { return s.store.Delete(id) }

func (s *SceneService) Recall(ctx context.Context, id string) (SceneRecallResult, error) {
	if err := ctx.Err(); err != nil {
		return SceneRecallResult{}, err
	}
	scene, err := s.find(id)
	if err != nil {
		return SceneRecallResult{}, err
	}
	transport, ok := s.transport.(SceneTransport)
	if !ok {
		return SceneRecallResult{}, errors.New("scene recall is unavailable for this transport")
	}
	return transport.RecallSceneState(ctx, scene.Snapshot)
}

func sceneSerials(values []string) ([]lifxdevice.Serial, error) {
	if len(values) == 0 || len(values) > maxSceneDevices {
		return nil, errors.New("scene requires 1..64 lights")
	}
	result := make([]lifxdevice.Serial, 0, len(values))
	seen := make(map[lifxdevice.Serial]bool)
	for _, value := range values {
		serial, err := parseDeviceSerial(Device{Serial: value})
		if err != nil || serial == (lifxdevice.Serial{}) {
			return nil, fmt.Errorf("invalid scene serial %q", value)
		}
		if seen[serial] {
			return nil, errors.New("duplicate scene device")
		}
		seen[serial] = true
		result = append(result, serial)
	}
	slices.SortFunc(result, func(a, b lifxdevice.Serial) int { return strings.Compare(a.String(), b.String()) })
	return result, nil
}

func (t *LifxTransport) CaptureSceneState(ctx context.Context, values []string) (lifxdevice.StateSnapshot, map[string]string, error) {
	serials, err := sceneSerials(values)
	if err != nil {
		return lifxdevice.StateSnapshot{}, nil, err
	}
	ctrl, err := t.requireController()
	if err != nil {
		return lifxdevice.StateSnapshot{}, nil, err
	}
	if err := t.ensureNetworkInterfaceAvailable(); err != nil {
		return lifxdevice.StateSnapshot{}, nil, err
	}
	var releases []func()
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	labels := make(map[string]string)
	for _, serial := range serials {
		unlock, err := t.lockDeviceCommand(ctx, serial)
		if err != nil {
			return lifxdevice.StateSnapshot{}, nil, err
		}
		releases = append(releases, unlock)
		d, ok := ctrl.GetDevice(serial)
		if !ok || (d.Type != lifxdevice.DeviceTypeLight && d.Type != lifxdevice.DeviceTypeHybrid) {
			return lifxdevice.StateSnapshot{}, nil, fmt.Errorf("scene target %s is not an available light", serial)
		}
		t.mu.RLock()
		_, appRunning := t.effects[serial.String()]
		firmware, tracked := t.firmware[serial.String()]
		t.mu.RUnlock()
		observed := mapLifxFirmwareEffect(d)
		if appRunning || tracked && !firmware.stopping || observed != nil && observed.Running {
			return lifxdevice.StateSnapshot{}, nil, fmt.Errorf("stop the effect on %s before capturing a scene", nameOrUnknown(d.Label, serial.String()))
		}
		labels[serial.String()] = nameOrUnknown(d.Label, serial.String())
	}
	state, err := ctrl.CaptureStateSnapshot(ctx, serials, lifxcontroller.SnapshotOptions{RequireFresh: true})
	if err != nil {
		return lifxdevice.StateSnapshot{}, nil, fmt.Errorf("capture scene state: %w", err)
	}
	if len(state.Devices) != len(serials) {
		return lifxdevice.StateSnapshot{}, nil, errors.New("scene capture is incomplete")
	}
	for _, d := range state.Devices {
		if _, ok := labels[d.Serial.String()]; !ok {
			return lifxdevice.StateSnapshot{}, nil, errors.New("scene capture contains an unexpected device")
		}
	}
	if err := validateScene(Scene{Name: "capture", Snapshot: state, Labels: labels}); err != nil {
		return lifxdevice.StateSnapshot{}, nil, err
	}
	return state, labels, nil
}

func (t *LifxTransport) RecallSceneState(ctx context.Context, state lifxdevice.StateSnapshot) (SceneRecallResult, error) {
	if err := validateScene(Scene{Name: "recall", Snapshot: state}); err != nil {
		return SceneRecallResult{}, err
	}
	ctrl, err := t.requireController()
	if err != nil {
		return SceneRecallResult{}, err
	}
	if err := t.ensureNetworkInterfaceAvailable(); err != nil {
		return SceneRecallResult{}, err
	}
	result := SceneRecallResult{Devices: []Device{}, Failures: []SceneRecallFailure{}}
	for _, saved := range state.Devices {
		device, changed, err := t.recallSceneDevice(ctx, ctrl, saved)
		if err != nil {
			if changed {
				t.mu.Lock()
				delete(t.cache, saved.Serial.String())
				delete(t.restores, saved.Serial.String())
				t.mu.Unlock()
			}
			result.Failures = append(result.Failures, SceneRecallFailure{Serial: saved.Serial.String(), Error: err.Error(), StateMayHaveChanged: changed})
		} else {
			result.Devices = append(result.Devices, device)
		}
	}
	return result, nil
}

func (t *LifxTransport) recallSceneDevice(ctx context.Context, ctrl lifxController, saved lifxdevice.DeviceStateSnapshot) (Device, bool, error) {
	unlock, err := t.lockDeviceCommand(ctx, saved.Serial)
	if err != nil {
		return Device{}, false, err
	}
	defer unlock()
	d, exists := ctrl.GetDevice(saved.Serial)
	if !exists || (d.Type != lifxdevice.DeviceTypeLight && d.Type != lifxdevice.DeviceTypeHybrid) {
		return Device{}, false, errors.New("light is unavailable on this LAN")
	}
	if _, err := ctrl.CaptureStateSnapshot(ctx, []lifxdevice.Serial{saved.Serial}, lifxcontroller.SnapshotOptions{RequireFresh: true, Timeout: time.Second}); err != nil {
		return Device{}, false, fmt.Errorf("observe before scene recall: %w", err)
	}
	d, exists = ctrl.GetDevice(saved.Serial)
	if !exists || (d.Type != lifxdevice.DeviceTypeLight && d.Type != lifxdevice.DeviceTypeHybrid) || d.LightType != saved.LightType {
		return Device{}, false, errors.New("light is unavailable or no longer compatible with this scene")
	}
	if saved.LightType == lifxdevice.LightTypeMultiZone && len(d.MultizoneProperties.Zones) != len(saved.Zones) {
		return Device{}, false, errors.New("zone count changed since scene capture")
	}
	if saved.LightType == lifxdevice.LightTypeMatrix {
		if d.MatrixProperties.Width != saved.MatrixWidth || len(d.MatrixProperties.ChainZones) != len(saved.MatrixChains) {
			return Device{}, false, errors.New("matrix geometry changed since scene capture")
		}
		for i, chain := range saved.MatrixChains {
			if len(d.MatrixProperties.ChainZones[i]) != len(chain) {
				return Device{}, false, errors.New("matrix geometry changed since scene capture")
			}
		}
	}
	before := mapLifxDevice(d, mapLifxGroupID(d, mapLifxLocationID(d)))
	after := mapLifxDevice(deviceWithEffectSnapshot(d, saved), before.GroupID)
	if lifxdevice.HasUplight(d) {
		mask, err := lifxeffects.LightPartPhysicalMask(d, lifxdevice.LightPartUplight)
		if err != nil {
			return Device{}, false, err
		}
		for index, cell := range mask {
			if cell.Brightness > 0 {
				after.Brightness = max(after.Brightness, float64(saved.MatrixChains[0][index].Brightness)/65535)
			}
		}
		after.On = saved.PoweredOn && after.Brightness > 0
	}
	t.mu.RLock()
	_, appRunning := t.effects[saved.Serial.String()]
	fw, tracked := t.firmware[saved.Serial.String()]
	revision := t.endpointRevision[saved.Serial.String()]
	t.mu.RUnlock()
	changed := false
	if !saved.PoweredOn && d.PoweredOn && (appRunning || tracked && !fw.stopping || before.FirmwareEffect != nil && before.FirmwareEffect.Running) {
		changed = true
		if err := sendEffectPowerOff(ctx, ctrl, saved.Serial, before); err != nil {
			return Device{}, changed, err
		}
	}
	if t.stopAppEffect(saved.Serial.String()) != nil {
		changed = true
	}
	if err := t.stopFirmwareEffectBeforeApp(ctx, ctrl, saved.Serial, before); err != nil {
		return Device{}, true, err
	}
	if err := ctx.Err(); err != nil {
		return Device{}, changed, err
	}
	// The native snapshot retains physical order, chain members and uplight.
	// One attempt only: successful UDP writes are not acknowledgements.
	duration := defaultColorTransitionDuration
	if !saved.PoweredOn {
		duration = 0
	}
	if err := ctrl.RestoreStateSnapshot(ctx, lifxdevice.StateSnapshot{Devices: []lifxdevice.DeviceStateSnapshot{saved}}, lifxcontroller.RestoreOptions{Duration: duration, Attempts: 1}); err != nil {
		return Device{}, true, fmt.Errorf("recall scene: %w", err)
	}
	if t.currentController() != ctrl {
		return Device{}, true, lifxcontroller.ErrEndpointChanged
	}
	t.mu.RLock()
	moved := t.endpointRevision[saved.Serial.String()] != revision
	t.mu.RUnlock()
	if moved {
		return Device{}, true, lifxcontroller.ErrEndpointChanged
	}
	after.EndpointRevision = revision
	after.FirmwareEffect = &FirmwareEffectState{Running: false}
	t.storeCachedDevice(after)
	t.storeRestoreDevice(after, time.Now().Add(3*time.Second))
	return after, true, nil
}
