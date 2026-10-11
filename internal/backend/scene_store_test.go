package backend

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

func testSceneStore(t *testing.T) *fileSceneStore {
	t.Helper()
	return &fileSceneStore{path: filepath.Join(t.TempDir(), "hikari", "scenes.json")}
}

func testSavedScene(t *testing.T) Scene {
	t.Helper()
	single := themeTestLight(t, "d073d501a2c3")
	single.PoweredOn = false
	matrix := themeUplightDevice(t, 265, 8, 8, lifxdevice.OrientationLeft, 41234)
	matrix.Serial, _ = lifxdevice.SerialFromHex("d073d501a2c4")
	return Scene{Name: "Evening", SavedAtMS: 1000, Snapshot: lifxdevice.NewStateSnapshot([]lifxdevice.Device{single, matrix}), Labels: map[string]string{single.Serial.String(): "Lamp", matrix.Serial.String(): "Ceiling"}}
}

func TestSceneStoreRoundTripsExactStateAndStableIdentity(t *testing.T) {
	store := testSceneStore(t)
	empty, err := store.Load()
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty load: %v", err)
	}
	input := testSavedScene(t)
	input.Name = "  Evening  "
	saved, err := store.Save(input)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := (&fileSceneStore{path: store.path}).Load()
	if err != nil || len(loaded) != 1 || loaded[0].ID != saved.ID || loaded[0].Name != "Evening" || !reflect.DeepEqual(loaded[0].Snapshot, input.Snapshot) {
		t.Fatalf("state or identity changed: %#v %v", loaded, err)
	}
	renamed, err := store.Rename(saved.ID, " Night ")
	if err != nil || renamed.ID != saved.ID || renamed.Name != "Night" || !reflect.DeepEqual(renamed.Snapshot, saved.Snapshot) {
		t.Fatal("rename changed state or identity")
	}
	info, err := os.Stat(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions=%o", info.Mode().Perm())
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(store.path), "*.tmp"))
	if len(files) != 0 {
		t.Fatal("atomic save left temporary files")
	}
	if err := store.Delete(saved.ID); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Load()
	if err != nil || len(loaded) != 0 {
		t.Fatal("delete failed")
	}
}

func TestSceneStorePersistsEditedMembership(t *testing.T) {
	store := testSceneStore(t)
	scene, err := store.Save(testSavedScene(t))
	if err != nil {
		t.Fatal(err)
	}
	scene.Snapshot.Devices[0].Serial, _ = lifxdevice.SerialFromHex("d073d501a2c9")
	scene.Labels = nil
	if _, err := store.Save(scene); err != nil {
		t.Fatal(err)
	}
	loaded, _ := store.Load()
	if loaded[0].Snapshot.Devices[0].Serial != scene.Snapshot.Devices[0].Serial {
		t.Fatal("edited membership was not saved")
	}
}

func TestCorruptOrFutureSceneDocumentsAreNeverOverwritten(t *testing.T) {
	for _, raw := range []string{`{"version":1,"scenes":`, `{"version":2,"scenes":[]}`, `{"version":1,"scenes":[{"id":"bad"}]}`} {
		t.Run(raw, func(t *testing.T) {
			store := testSceneStore(t)
			if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(store.path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := store.Load(); err == nil {
				t.Fatal("invalid document accepted")
			}
			if _, err := store.Save(testSavedScene(t)); err == nil {
				t.Fatal("invalid document overwritten")
			}
			if err := store.Delete("scene-existing"); err == nil {
				t.Fatal("invalid document rewritten on delete")
			}
			data, _ := os.ReadFile(store.path)
			if string(data) != raw {
				t.Fatal("original document lost")
			}
		})
	}
}

func TestSceneValidationRejectsMalformedOrOversizedState(t *testing.T) {
	for _, modify := range []func(*Scene){
		func(s *Scene) { s.Name = " " },
		func(s *Scene) { s.Snapshot.Devices = nil },
		func(s *Scene) { s.Snapshot.Devices[1].Serial = s.Snapshot.Devices[0].Serial },
		func(s *Scene) { s.Snapshot.Devices[1].MatrixWidth = 0 },
		func(s *Scene) { s.Snapshot.Devices[1].MatrixChains[0] = s.Snapshot.Devices[1].MatrixChains[0][:63] },
		func(s *Scene) { s.Snapshot.Devices[0].Color.Brightness = 101 },
		func(s *Scene) { s.Labels["unknown"] = "Extra" },
		func(s *Scene) { s.Snapshot.Devices[1].MatrixChains = make([][]packets.LightHsbk, 65) },
	} {
		scene := testSavedScene(t)
		modify(&scene)
		if _, err := testSceneStore(t).Save(scene); err == nil {
			t.Fatal("invalid scene accepted")
		}
	}
}

func TestSceneSummaryDoesNotExposePhysicalBuffers(t *testing.T) {
	summary := testSavedScene(t).Summary()
	data, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 || fields["snapshot"] != nil {
		t.Fatal("scene list leaked restorable protocol state")
	}
	if summary.Targets[0].Serial != "d073d501a2c3" || summary.Targets[0].Label != "Lamp" {
		t.Fatal("summary lost stable serial/label")
	}
}

func TestSceneSummaryShowsSavedWhiteAndPatternStateWithoutMutatingSnapshot(t *testing.T) {
	scene := testSavedScene(t)
	scene.Snapshot.Devices[0].Color = lifxdevice.Color{Hue: 220, Saturation: 0, Brightness: 40, Kelvin: 2000}
	pattern := &scene.Snapshot.Devices[1]
	pattern.PoweredOn = true
	pattern.Color.Brightness = 99
	pattern.MatrixChains = [][]packets.LightHsbk{{{Hue: 0, Saturation: 65535, Brightness: 32768, Kelvin: 3500}}, {{Hue: 40000, Saturation: 65535, Brightness: 65535, Kelvin: 3500}}}
	before := cloneStateSnapshot(scene.Snapshot)
	summary := scene.Summary()
	white := summary.Targets[0]
	if white.On || white.Brightness != 0.4 || len(white.Colors) != 1 || white.Colors[0].Kelvin != 2000 || white.Colors[0].S != 0 || white.Colors[0].L != 1 {
		t.Fatalf("white=%#v", white)
	}
	got := summary.Targets[1]
	if !got.On || len(got.Colors) != 2 || math.Abs(got.Brightness-0.75) > 0.001 {
		t.Fatalf("pattern=%#v", got)
	}
	if !reflect.DeepEqual(before, scene.Snapshot) {
		t.Fatal("summary modified saved state")
	}
}

func TestSceneSummaryBoundsRepresentativeSwatches(t *testing.T) {
	scene := testSavedScene(t)
	pattern := &scene.Snapshot.Devices[1]
	pattern.MatrixChains = [][]packets.LightHsbk{make([]packets.LightHsbk, 64)}
	for i := range pattern.MatrixChains[0] {
		pattern.MatrixChains[0][i] = packets.LightHsbk{Hue: uint16(i * 1000), Saturation: 65535, Brightness: 65535}
	}
	if got := scene.Summary().Targets[1].Colors; len(got) != 5 || got[0].H != 0 || got[4].H <= got[0].H {
		t.Fatalf("swatches=%#v", got)
	}
}
