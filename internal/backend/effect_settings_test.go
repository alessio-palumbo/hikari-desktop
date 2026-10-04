package backend

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func TestEffectSettingsPersistPerDeviceAndEffect(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hikari", "effect-settings.json")
	store := &fileEffectSettingsStore{path: path}
	if settings, err := store.Load("d073d501a2c3"); err != nil || len(settings) != 0 {
		t.Fatalf("missing file: %#v %v", settings, err)
	}
	requests := []SaveDeviceEffectPreferenceRequest{
		{Serial: "d073d501a2c3", Effect: DeviceEffectScanner, EffectPreference: EffectPreference{SpeedMS: 6000, Params: map[string]any{"background_brightness_factor": .6}}},
		{Serial: "d073d501a2c3", Effect: DeviceEffectMove, EffectPreference: EffectPreference{SpeedMS: 20000}},
		{Serial: "d073d501a2c4", Effect: DeviceEffectScanner, EffectPreference: EffectPreference{SpeedMS: 4000}},
	}
	for _, req := range requests {
		if err := store.Save(req); err != nil {
			t.Fatal(err)
		}
	}
	restarted := &fileEffectSettingsStore{path: path}
	first, err := restarted.Load("D0:73:D5:01:A2:C3")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || !reflect.DeepEqual(first[DeviceEffectScanner], requests[0].EffectPreference) {
		t.Fatalf("first device: %#v", first)
	}
	second, err := restarted.Load(requests[2].Serial)
	if err != nil || len(second) != 1 || second[DeviceEffectScanner].SpeedMS != 4000 {
		t.Fatalf("second device: %#v %v", second, err)
	}
	first[DeviceEffectScanner].Params["background_brightness_factor"] = .1
	again, _ := restarted.Load(requests[0].Serial)
	if again[DeviceEffectScanner].Params["background_brightness_factor"] != .6 {
		t.Fatal("caller mutated persistent state")
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temporary file left behind: %#v", entries)
	}
}

func TestEffectSettingsRejectInvalidValuesWithoutOverwriting(t *testing.T) {
	store := &fileEffectSettingsStore{path: filepath.Join(t.TempDir(), "effect-settings.json")}
	valid := SaveDeviceEffectPreferenceRequest{Serial: "d073d501a2c3", Effect: DeviceEffectScanner, EffectPreference: EffectPreference{SpeedMS: 4000}}
	if err := store.Save(valid); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(store.path)
	for _, preference := range []EffectPreference{
		{SpeedMS: 0}, {SpeedMS: 31000},
		{SpeedMS: 4000, Params: map[string]any{"palette": 1}},
		{SpeedMS: 4000, Params: map[string]any{"background_brightness_factor": 2}},
		{SpeedMS: 4000, Params: map[string]any{"peak_brightness_factor": math.NaN()}},
	} {
		req := valid
		req.EffectPreference = preference
		if err := store.Save(req); err == nil {
			t.Fatalf("invalid settings accepted: %#v", preference)
		}
	}
	after, _ := os.ReadFile(store.path)
	if string(before) != string(after) {
		t.Fatal("invalid write changed existing settings")
	}
}

func TestEffectSettingsCorruptOrNewerDocumentIsNotOverwritten(t *testing.T) {
	for _, data := range []string{`broken`, `{"version":2,"devices":{}}`} {
		store := &fileEffectSettingsStore{path: filepath.Join(t.TempDir(), "effect-settings.json")}
		if err := os.WriteFile(store.path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load("d073d501a2c3"); err == nil {
			t.Fatal("invalid document accepted")
		}
		if err := store.Save(SaveDeviceEffectPreferenceRequest{Serial: "d073d501a2c3", Effect: DeviceEffectMove, EffectPreference: EffectPreference{SpeedMS: 20000}}); err == nil {
			t.Fatal("invalid document overwritten")
		}
		after, _ := os.ReadFile(store.path)
		if string(after) != data {
			t.Fatal("original document lost")
		}
	}
}

func TestEffectSettingsIgnoreUnknownEffectsAndUnsupportedParameters(t *testing.T) {
	store := &fileEffectSettingsStore{path: filepath.Join(t.TempDir(), "effect-settings.json")}
	data := `{"version":1,"devices":{"d073d501a2c3":{"unknown":{"speedMs":2000},"scanner":{"speedMs":4000,"params":{"removed_field":1}},"move":{"speedMs":20000}}}}`
	if err := os.WriteFile(store.path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := store.Load("d073d501a2c3")
	if err != nil || len(settings) != 1 || settings[DeviceEffectMove].SpeedMS != 20000 {
		t.Fatalf("invalid fallback: %#v %v", settings, err)
	}
}

func TestEffectSettingsConcurrentSavesKeepOtherEffects(t *testing.T) {
	store := &fileEffectSettingsStore{path: filepath.Join(t.TempDir(), "effect-settings.json")}
	var wait sync.WaitGroup
	for _, effect := range []DeviceEffect{DeviceEffectMove, DeviceEffectSnake, DeviceEffectRing, DeviceEffectScanner} {
		wait.Add(1)
		go func(effect DeviceEffect) {
			defer wait.Done()
			if err := store.Save(SaveDeviceEffectPreferenceRequest{Serial: "d073d501a2c3", Effect: effect, EffectPreference: EffectPreference{SpeedMS: 2000}}); err != nil {
				t.Error(err)
			}
		}(effect)
	}
	wait.Wait()
	settings, err := store.Load("d073d501a2c3")
	if err != nil || len(settings) != 4 {
		t.Fatalf("lost saved effect: %#v %v", settings, err)
	}
}

func TestEffectSettingsRoundTripChoicesAlongsideExistingNumericSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "effect-settings.json")
	store := &fileEffectSettingsStore{path: path}
	for _, req := range []SaveDeviceEffectPreferenceRequest{
		{Serial: "d073d501a2c3", Effect: DeviceEffectFlow, EffectPreference: EffectPreference{SpeedMS: 4000, Params: map[string]any{"axis": "vertical", "direction": "reverse"}}},
		{Serial: "d073d501a2c3", Effect: DeviceEffectFrames, EffectPreference: EffectPreference{SpeedMS: 2000, Params: map[string]any{"direction": "outwards"}}},
		{Serial: "d073d501a2c3", Effect: DeviceEffectScanner, EffectPreference: EffectPreference{SpeedMS: 6000, Params: map[string]any{"background_brightness_factor": .6}}},
	} {
		if err := store.Save(req); err != nil {
			t.Fatal(err)
		}
	}
	loaded, err := (&fileEffectSettingsStore{path: path}).Load("d073d501a2c3")
	if err != nil || loaded[DeviceEffectFlow].Params["direction"] != "reverse" || loaded[DeviceEffectFlow].Params["axis"] != "vertical" || loaded[DeviceEffectFrames].Params["direction"] != "outwards" || loaded[DeviceEffectScanner].Params["background_brightness_factor"] != .6 {
		t.Fatalf("roundtrip: %#v %v", loaded, err)
	}
	for _, params := range []map[string]any{{"direction": "invalid"}, {"axis": 1}, {"direction": nil}, {"sampling": "step"}} {
		if err := store.Save(SaveDeviceEffectPreferenceRequest{Serial: "d073d501a2c3", Effect: DeviceEffectFlow, EffectPreference: EffectPreference{SpeedMS: 4000, Params: params}}); err == nil {
			t.Fatalf("invalid choices persisted: %#v", params)
		}
	}
}
