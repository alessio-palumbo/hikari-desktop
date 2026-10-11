package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

type sceneServiceTransport struct {
	*MockTransport
	state      lifxdevice.StateSnapshot
	requested  []string
	captureErr error
	recalled   lifxdevice.StateSnapshot
}

func (t *sceneServiceTransport) CaptureSceneState(ctx context.Context, serials []string) (lifxdevice.StateSnapshot, map[string]string, error) {
	if err := ctx.Err(); err != nil {
		return lifxdevice.StateSnapshot{}, nil, err
	}
	t.requested = append([]string(nil), serials...)
	if t.captureErr != nil {
		return lifxdevice.StateSnapshot{}, nil, t.captureErr
	}
	labels := make(map[string]string)
	for _, state := range t.state.Devices {
		labels[state.Serial.String()] = "Saved light"
	}
	return cloneStateSnapshot(t.state), labels, nil
}

func (t *sceneServiceTransport) RecallSceneState(ctx context.Context, state lifxdevice.StateSnapshot) (SceneRecallResult, error) {
	if err := ctx.Err(); err != nil {
		return SceneRecallResult{}, err
	}
	t.recalled = cloneStateSnapshot(state)
	return SceneRecallResult{Devices: []Device{}, Failures: []SceneRecallFailure{}}, nil
}

func TestSceneServiceCaptureRenameRecaptureAndRecallKeepFixedMembership(t *testing.T) {
	initial := testSavedScene(t)
	transport := &sceneServiceTransport{MockTransport: NewMockTransport(), state: initial.Snapshot}
	store := testSceneStore(t)
	service := NewSceneService(transport, store)
	serials := []string{initial.Snapshot.Devices[0].Serial.String(), initial.Snapshot.Devices[1].Serial.String()}
	scene, err := service.Capture(context.Background(), CaptureSceneRequest{Name: "Night", Serials: serials})
	if err != nil || len(scene.Targets) != 2 {
		t.Fatalf("capture=%#v %v", scene, err)
	}
	renamed, err := service.Rename(RenameSceneRequest{ID: scene.ID, Name: "Evening"})
	if err != nil || renamed.ID != scene.ID {
		t.Fatal("rename changed identity")
	}
	transport.state.Devices[0].Color.Brightness = 25
	updated, err := service.Capture(context.Background(), CaptureSceneRequest{ID: scene.ID, Name: "Ignored", Serials: []string{"d073d501ffff"}})
	if err != nil || updated.Name != "Evening" || !reflect.DeepEqual(transport.requested, serials) {
		t.Fatalf("recapture replaced membership: %v", err)
	}
	if _, err := service.Recall(context.Background(), scene.ID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(transport.recalled, transport.state) {
		t.Fatal("recall did not use saved physical state")
	}
	if err := service.Delete(scene.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := service.List()
	if err != nil || len(entries) != 0 {
		t.Fatal("scene was not deleted")
	}
}

func TestSceneCaptureFailureLeavesPreviousSceneUntouched(t *testing.T) {
	initial := testSavedScene(t)
	store := testSceneStore(t)
	scene, err := store.Save(initial)
	if err != nil {
		t.Fatal(err)
	}
	transport := &sceneServiceTransport{MockTransport: NewMockTransport(), captureErr: errors.New("state not observed")}
	service := NewSceneService(transport, store)
	if _, err := service.Capture(context.Background(), CaptureSceneRequest{ID: scene.ID}); err == nil {
		t.Fatal("capture failure hidden")
	}
	loaded, _ := store.Load()
	if !reflect.DeepEqual(loaded[0].Snapshot, initial.Snapshot) {
		t.Fatal("failed update replaced saved state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Recall(ctx, scene.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestSceneEditCapturesOnlyAddedLightsAndPreservesRetainedState(t *testing.T) {
	store := testSceneStore(t)
	initial, err := store.Save(testSavedScene(t))
	if err != nil {
		t.Fatal(err)
	}
	retained := initial.Snapshot.Devices[0]
	added := retained
	added.Serial, _ = lifxdevice.SerialFromHex("d073d501a2c9")
	added.Color.Brightness = 12
	transport := &sceneServiceTransport{MockTransport: NewMockTransport(), state: lifxdevice.StateSnapshot{Devices: []lifxdevice.DeviceStateSnapshot{added}}}
	service := NewSceneService(transport, store)
	updated, err := service.Edit(context.Background(), EditSceneRequest{ID: initial.ID, Name: "Edited", Serials: []string{retained.Serial.String(), added.Serial.String()}})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != initial.ID || updated.Name != "Edited" || !reflect.DeepEqual(transport.requested, []string{added.Serial.String()}) {
		t.Fatal("edit did not capture only additions")
	}
	loaded, _ := store.Load()
	for _, state := range loaded[0].Snapshot.Devices {
		if state.Serial == retained.Serial && !reflect.DeepEqual(state, retained) {
			t.Fatal("retained state changed")
		}
		if state.Serial == initial.Snapshot.Devices[1].Serial {
			t.Fatal("removed member retained")
		}
	}
	transport.captureErr = errors.New("offline")
	transport.requested = nil
	if _, err := service.Edit(context.Background(), EditSceneRequest{ID: initial.ID, Name: "Only retained", Serials: []string{retained.Serial.String()}}); err != nil {
		t.Fatal(err)
	}
	if len(transport.requested) != 0 {
		t.Fatal("retained member was recaptured")
	}
	before, _ := store.Load()
	if _, err := service.Edit(context.Background(), EditSceneRequest{ID: initial.ID, Name: "Failed", Serials: []string{retained.Serial.String(), added.Serial.String()}}); err == nil {
		t.Fatal("failed addition accepted")
	}
	after, _ := store.Load()
	if !reflect.DeepEqual(before, after) {
		t.Fatal("failed edit changed stored scene")
	}
	if _, err := service.Edit(context.Background(), EditSceneRequest{ID: initial.ID, Name: "Empty"}); err == nil {
		t.Fatal("empty membership accepted")
	}
}

func TestSceneCaptureUsesFreshNativePhysicalStateWithoutControlWrites(t *testing.T) {
	d := themeUplightDevice(t, 265, 8, 8, lifxdevice.OrientationRight, 34567)
	d.PoweredOn = false
	base := &fakeLifxController{devices: []lifxdevice.Device{d}}
	fresh := false
	ctrl := &snapshotBoundaryController{fakeLifxController: base, capture: func(ctx context.Context, serials []lifxdevice.Serial, options lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
		fresh = options.RequireFresh
		return base.CaptureStateSnapshot(ctx, serials, options)
	}}
	transport := newTestLifxTransport(t, ctrl)
	state, labels, err := transport.CaptureSceneState(context.Background(), []string{d.Serial.String()})
	if err != nil || !fresh || !reflect.DeepEqual(state, lifxdevice.NewStateSnapshot([]lifxdevice.Device{d})) || labels[d.Serial.String()] == "" {
		t.Fatalf("capture changed state: %v", err)
	}
	if len(base.sentMessages()) != 0 || len(base.restoredStateSnapshots()) != 0 {
		t.Fatal("capturing sent control messages")
	}
}

func TestSceneCaptureRejectsRunningEffectsAndMissingMembers(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := newTestLifxTransport(t, ctrl)
	transport.effects[d.Serial.String()] = runningAppEffect{previous: mapLifxDevice(d, "desk"), cancel: func() {}}
	if _, _, err := transport.CaptureSceneState(context.Background(), []string{d.Serial.String()}); err == nil {
		t.Fatal("active animation captured")
	}
	delete(transport.effects, d.Serial.String())
	if _, _, err := transport.CaptureSceneState(context.Background(), []string{d.Serial.String(), "d073d501a2c9"}); err == nil {
		t.Fatal("missing target silently omitted")
	}
	if len(ctrl.sentMessages()) != 0 {
		t.Fatal("failed capture controlled lights")
	}
}

func TestSceneRecallRestoresPhysicalUplightAndUsesNativeOffOrdering(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[on], func(t *testing.T) {
			d := themeUplightDevice(t, 265, 8, 8, lifxdevice.OrientationLeft, 41234)
			d.PoweredOn = on
			saved := lifxdevice.NewStateSnapshot([]lifxdevice.Device{d})
			current := d.Clone()
			current.PoweredOn = !on
			for i := range current.MatrixProperties.ChainZones[0] {
				current.MatrixProperties.ChainZones[0][i].Hue = 0
			}
			base := &fakeLifxController{devices: []lifxdevice.Device{current}}
			ctrl := &restoreOptionsController{fakeLifxController: base}
			transport := newTestLifxTransport(t, ctrl)
			result, err := transport.RecallSceneState(context.Background(), saved)
			if err != nil || len(result.Devices) != 1 || len(result.Failures) != 0 {
				t.Fatalf("recall=%#v %v", result, err)
			}
			if !reflect.DeepEqual(base.restoredStateSnapshots(), []lifxdevice.StateSnapshot{saved}) || ctrl.options.Attempts != 1 {
				t.Fatal("native physical snapshot or single-attempt semantics changed")
			}
			if !on && ctrl.options.Duration != 0 || on && ctrl.options.Duration != defaultColorTransitionDuration {
				t.Fatal("restore transition policy changed")
			}
		})
	}
}

type sceneRestoreController struct {
	*fakeLifxController
	failSerial lifxdevice.Serial
}

func (c *sceneRestoreController) RestoreStateSnapshot(ctx context.Context, state lifxdevice.StateSnapshot, options lifxcontroller.RestoreOptions) error {
	if state.Devices[0].Serial == c.failSerial {
		return errors.New("send failed")
	}
	return c.fakeLifxController.RestoreStateSnapshot(ctx, state, options)
}

func TestSceneRecallMissingAndFailedMembersDoNotBlockOtherLights(t *testing.T) {
	a, b, missing := themeTestLight(t, "d073d501a2c3"), themeTestLight(t, "d073d501a2c4"), themeTestLight(t, "d073d501a2c5")
	ctrl := &sceneRestoreController{fakeLifxController: &fakeLifxController{devices: []lifxdevice.Device{a, b}}, failSerial: a.Serial}
	transport := newTestLifxTransport(t, ctrl)
	transport.storeRestoreDevice(mapLifxDevice(a, "desk"), time.Now().Add(time.Second))
	result, err := transport.RecallSceneState(context.Background(), lifxdevice.NewStateSnapshot([]lifxdevice.Device{a, b, missing}))
	if err != nil || len(result.Devices) != 1 || result.Devices[0].Serial != b.Serial.String() || len(result.Failures) != 2 {
		t.Fatalf("partial recall=%#v %v", result, err)
	}
	if !result.Failures[0].StateMayHaveChanged || result.Failures[1].StateMayHaveChanged {
		t.Fatal("partial-send and unavailable target were conflated")
	}
	if transport.cachedDevice(a.Serial.String()) != nil || len(transport.restores) != 1 {
		t.Fatal("failed target retained a stale-state guard")
	}
}

func TestSceneRecallRejectsChangedGeometryBeforeRestore(t *testing.T) {
	d := themeUplightDevice(t, 265, 8, 8, lifxdevice.OrientationRightSideUp, 30000)
	saved := lifxdevice.NewStateSnapshot([]lifxdevice.Device{d})
	d.MatrixProperties.Width = 4
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	result, err := newTestLifxTransport(t, ctrl).RecallSceneState(context.Background(), saved)
	if err != nil || len(result.Failures) != 1 || result.Failures[0].StateMayHaveChanged || len(ctrl.restoredStateSnapshots()) != 0 {
		t.Fatal("incompatible geometry was sent")
	}
}

func TestSceneRecallPreservesStripZonesAndEveryMatrixChain(t *testing.T) {
	strip := themeTestLight(t, "d073d501a2c3")
	strip.LightType = lifxdevice.LightTypeMultiZone
	strip.MultizoneProperties.Zones = []packets.LightHsbk{{Hue: 1000, Saturation: 12000, Brightness: 0, Kelvin: 2000}, {Hue: 45000, Saturation: 62000, Brightness: 41234, Kelvin: 3500}}
	matrix := previewTestDevice(55, 8, 8, 2)
	matrix.Serial, _ = lifxdevice.SerialFromHex("d073d501a2c4")
	matrix.Type = lifxdevice.DeviceTypeLight
	matrix.MatrixProperties.ChainZones = [][]packets.LightHsbk{make([]packets.LightHsbk, 64), make([]packets.LightHsbk, 64)}
	matrix.MatrixProperties.ChainOrientations = []lifxdevice.Orientation{lifxdevice.OrientationLeft, lifxdevice.OrientationRight}
	for chain := range matrix.MatrixProperties.ChainZones {
		for i := range matrix.MatrixProperties.ChainZones[chain] {
			matrix.MatrixProperties.ChainZones[chain][i] = packets.LightHsbk{Hue: uint16(1000 + chain*30000 + i), Saturation: 50000, Brightness: uint16(i * 700), Kelvin: 3500}
		}
	}
	saved := lifxdevice.NewStateSnapshot([]lifxdevice.Device{strip, matrix})
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{strip, matrix}}
	result, err := newTestLifxTransport(t, ctrl).RecallSceneState(context.Background(), saved)
	if err != nil || len(result.Devices) != 2 || len(result.Failures) != 0 {
		t.Fatalf("recall=%#v %v", result, err)
	}
	restored := ctrl.restoredStateSnapshots()
	if len(restored) != 2 || !reflect.DeepEqual(restored[0].Devices[0], saved.Devices[0]) || !reflect.DeepEqual(restored[1].Devices[0], saved.Devices[1]) {
		t.Fatal("zone order, brightness, orientation-independent physical order, or chain members changed")
	}
}
