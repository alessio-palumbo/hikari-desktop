package main

import (
	"hikari-desktop/internal/backend"
	"testing"
)

type testEffectSettingsStore struct {
	preferences map[backend.DeviceEffect]backend.EffectPreference
	saved       backend.SaveDeviceEffectPreferenceRequest
}

func (s *testEffectSettingsStore) Load(string) (map[backend.DeviceEffect]backend.EffectPreference, error) {
	return s.preferences, nil
}
func (s *testEffectSettingsStore) Save(req backend.SaveDeviceEffectPreferenceRequest) error {
	s.saved = req
	return nil
}

func TestAppEffectPreferencesNeverStartOrControlDevices(t *testing.T) {
	transport := &recordingTransport{}
	app := NewAppWithTransport(transport)
	store := &testEffectSettingsStore{preferences: map[backend.DeviceEffect]backend.EffectPreference{
		backend.DeviceEffectScanner: {SpeedMS: 6000, Params: map[string]float64{"background_brightness_factor": .6}},
	}}
	app.effectSettings = store
	preferences, err := app.GetDeviceEffectPreferences("d073d501a2c3")
	if err != nil || preferences[backend.DeviceEffectScanner].SpeedMS != 6000 {
		t.Fatalf("load: %#v %v", preferences, err)
	}
	req := backend.SaveDeviceEffectPreferenceRequest{Serial: "d073d501a2c3", Effect: backend.DeviceEffectMove, EffectPreference: backend.EffectPreference{SpeedMS: 25000}}
	if err := app.SaveDeviceEffectPreference(req); err != nil || store.saved.SpeedMS != 25000 {
		t.Fatalf("save: %#v %v", store.saved, err)
	}
	if transport.startEffectCalled || transport.stopEffectCalled || transport.setCalled || transport.startCalled || transport.snapshotCalled {
		t.Fatal("preferences triggered device activity")
	}
}

type smallEffectParameterTransport struct{ *recordingTransport }

func (t *smallEffectParameterTransport) EffectParameters(string, backend.DeviceEffect) ([]backend.EffectParameter, error) {
	return []backend.EffectParameter{{Key: "size", Min: 1, Max: 2, Step: 1, Default: 2, Value: 2}}, nil
}

func TestAppEffectPreferencesRespectCurrentDeviceDimensions(t *testing.T) {
	app := NewAppWithTransport(&smallEffectParameterTransport{&recordingTransport{}})
	app.effectSettings = &testEffectSettingsStore{preferences: map[backend.DeviceEffect]backend.EffectPreference{
		backend.DeviceEffectSnake: {SpeedMS: 6000, Params: map[string]float64{"size": 5}},
	}}
	preferences, err := app.GetDeviceEffectPreferences("d073d501a2c3")
	if err != nil || preferences[backend.DeviceEffectSnake].Params["size"] != 2 {
		t.Fatalf("unclamped preference: %#v %v", preferences, err)
	}
}
