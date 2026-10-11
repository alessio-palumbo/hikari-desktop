package main

import (
	"context"
	"reflect"
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"hikari-desktop/internal/backend"
)

type appSceneStore struct{ scene backend.Scene }

func (s *appSceneStore) Load() ([]backend.Scene, error) {
	if s.scene.ID == "" {
		return []backend.Scene{}, nil
	}
	return []backend.Scene{s.scene}, nil
}
func (s *appSceneStore) Save(scene backend.Scene) (backend.Scene, error) {
	scene.ID = "scene-test"
	s.scene = scene
	return scene, nil
}
func (s *appSceneStore) Rename(_ string, name string) (backend.Scene, error) {
	s.scene.Name = name
	return s.scene, nil
}
func (s *appSceneStore) Delete(string) error { s.scene = backend.Scene{}; return nil }

type appSceneTransport struct {
	*recordingTransport
	state          lifxdevice.StateSnapshot
	recalled       lifxdevice.StateSnapshot
	captureBounded bool
	recallBounded  bool
}

func (t *appSceneTransport) CaptureSceneState(ctx context.Context, _ []string) (lifxdevice.StateSnapshot, map[string]string, error) {
	_, t.captureBounded = ctx.Deadline()
	return t.state, nil, nil
}
func (t *appSceneTransport) RecallSceneState(ctx context.Context, state lifxdevice.StateSnapshot) (backend.SceneRecallResult, error) {
	_, t.recallBounded = ctx.Deadline()
	t.recalled = state
	return backend.SceneRecallResult{Devices: []backend.Device{}, Failures: []backend.SceneRecallFailure{}}, nil
}

func TestAppSceneBindingsDelegateWithoutLAN(t *testing.T) {
	serial, _ := lifxdevice.SerialFromHex("d073d501a2c3")
	transport := &appSceneTransport{recordingTransport: &recordingTransport{}, state: lifxdevice.StateSnapshot{Devices: []lifxdevice.DeviceStateSnapshot{{Serial: serial, LightType: lifxdevice.LightTypeSingleZone, Color: lifxdevice.Color{Brightness: 40, Kelvin: 3500}}}}}
	app := NewAppWithTransport(transport)
	app.scenes = backend.NewSceneService(transport, &appSceneStore{})
	scene, err := app.CaptureScene(backend.CaptureSceneRequest{Name: "Desk", Serials: []string{serial.String()}})
	if err != nil || !transport.captureBounded {
		t.Fatalf("capture=%#v %v", scene, err)
	}
	listed, err := app.GetScenes()
	if err != nil || len(listed) != 1 {
		t.Fatal("scene list did not use storage")
	}
	if _, err := app.RenameScene(backend.RenameSceneRequest{ID: scene.ID, Name: "Evening"}); err != nil {
		t.Fatal(err)
	}
	if edited, err := app.EditScene(backend.EditSceneRequest{ID: scene.ID, Name: "Edited", Serials: []string{serial.String()}}); err != nil || edited.Name != "Edited" {
		t.Fatalf("scene edit=%#v %v", edited, err)
	}
	if _, err := app.RecallScene(scene.ID); err != nil || !transport.recallBounded || !reflect.DeepEqual(transport.recalled, transport.state) {
		t.Fatal("scene recall did not delegate the saved state")
	}
	if err := app.DeleteScene(scene.ID); err != nil {
		t.Fatal(err)
	}
	listed, _ = app.GetScenes()
	if len(listed) != 0 {
		t.Fatal("scene delete did not use storage")
	}
}
