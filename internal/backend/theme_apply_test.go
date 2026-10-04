package backend

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	lifxthemes "github.com/alessio-palumbo/lifxlan-go/pkg/themes"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

type themeSendController struct {
	*fakeLifxController
	send func(lifxdevice.Serial, *protocol.Message) error
}

func (c *themeSendController) Send(serial lifxdevice.Serial, msg *protocol.Message) error {
	return c.send(serial, msg)
}

func TestThemeApplyPrimesOffDeviceAndPreservesBrightness(t *testing.T) {
	for _, on := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "on"}[on], func(t *testing.T) {
			d := themeTestLight(t, "d073d501a2c3")
			d.PoweredOn = on
			ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
			transport := newTestLifxTransport(t, ctrl)
			result, err := transport.ApplyTheme(context.Background(), testThemeRequest(d))
			if err != nil || len(result.Devices) != 1 || len(result.Failures) != 0 {
				t.Fatalf("apply: %#v %v", result, err)
			}
			if !result.Devices[0].On || math.Abs(result.Devices[0].Brightness-.4) > .0001 {
				t.Fatal("theme did not preserve brightness and turn on")
			}
			sends := ctrl.sentMessages()
			if _, ok := sends[0].msg.Payload.(*packets.LightSetWaveformOptional); !ok {
				t.Fatal("first command was not colour")
			}
			if on && len(sends) != 1 {
				t.Fatal("on device received redundant power command")
			}
			if !on {
				if len(sends) != 2 {
					t.Fatal("off device not powered on")
				}
				if power, ok := sends[1].msg.Payload.(*packets.DeviceSetPower); !ok || power.Level == 0 {
					t.Fatal("power-on did not follow colour")
				}
			}
			stale := DeviceSnapshot{Devices: []Device{mapLifxDevice(d, result.Devices[0].GroupID)}}
			transport.reconcileRestoreSnapshot(&stale, time.Now())
			if !reflect.DeepEqual(stale.Devices[0], result.Devices[0]) {
				t.Fatal("stale LAN observation overwrote theme")
			}
		})
	}
}

func TestThemeApplyMatchesLocalPreviewAcrossOrientedChain(t *testing.T) {
	d := previewTestDevice(55, 8, 8, 2)
	d.Serial, _ = lifxdevice.SerialFromHex("d073d501a2c3")
	d.Type = lifxdevice.DeviceTypeLight
	d.PoweredOn = true
	d.MatrixProperties.ChainOrientations = []lifxdevice.Orientation{lifxdevice.OrientationRight, lifxdevice.OrientationLeft}
	d.MatrixProperties.ChainZones = make([][]packets.LightHsbk, 2)
	for chain := range 2 {
		for i := range 64 {
			d.MatrixProperties.ChainZones[chain] = append(d.MatrixProperties.ChainZones[chain], (lifxeffects.Color{Hue: 120, Saturation: 90, Brightness: float64((i + chain*20) % 80), Kelvin: 3500}).ToDeviceColor())
		}
	}
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := newTestLifxTransport(t, ctrl)
	req := testThemeRequest(d)
	req.Variation, req.Seed, req.MatrixLayout = 3, 19, lifxthemes.MatrixSpatial
	preview, err := transport.PreviewTheme(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	result, err := transport.ApplyTheme(context.Background(), req)
	if err != nil || len(result.Failures) != 0 || len(result.Devices) != 1 {
		t.Fatalf("apply: %#v %v", result, err)
	}
	surface := lifxdevice.SurfaceFromDevice(d)
	state := lifxeffects.NewPhysicalColorState(surface)
	chains := map[uint8]bool{}
	for _, send := range ctrl.sentMessages() {
		if p, ok := send.msg.Payload.(*packets.TileSet64); ok {
			if p.Length != 1 {
				t.Fatal("per-chain theme write repeated across other tiles")
			}
			chains[p.TileIndex] = true
		}
		if err := applyPreviewPacket(&state, surface, send.msg); err != nil {
			t.Fatal(err)
		}
	}
	if len(chains) != 2 {
		t.Fatal("theme did not address both chain members")
	}
	frame, err := lifxeffects.AdaptPhysicalColorStateToFrame(state, surface, 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, color := range frame.Colors {
		want := preview.Devices[0].Colors[i]
		if math.Abs(color.Brightness/100-want.L) > .0001 || math.Abs(color.Hue-want.H) > .01 {
			t.Fatalf("preview differs from send at %d", i)
		}
	}
}

func TestThemeApplyStopsHikariEffectWithoutRestoringOldColors(t *testing.T) {
	transport, ctrl, current := parameterTestTransport(t)
	d := ctrl.devices[0].Clone()
	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectBreathe}); err != nil {
		t.Fatal(err)
	}
	result, err := transport.ApplyTheme(context.Background(), testThemeRequest(d))
	if err != nil || len(result.Failures) != 0 {
		t.Fatalf("apply: %#v %v", result, err)
	}
	if len(transport.effects) != 0 || len(ctrl.restoredStateSnapshots()) != 0 {
		t.Fatal("theme restarted or restored old effect")
	}
	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: result.Devices[0]}); err != nil {
		t.Fatal(err)
	}
	if len(ctrl.restoredStateSnapshots()) != 0 {
		t.Fatal("later stop restored pre-theme colours")
	}
}

func TestThemeApplyStopsExternalFirmwareBeforeColor(t *testing.T) {
	transport, ctrl, current := parameterTestTransport(t)
	current.FirmwareEffect = &FirmwareEffectState{Running: true, Effect: DeviceEffectMorph}
	transport.storeCachedDevice(current)
	result, err := transport.ApplyTheme(context.Background(), testThemeRequest(ctrl.devices[0]))
	if err != nil || len(result.Failures) != 0 {
		t.Fatalf("apply: %#v %v", result, err)
	}
	sends := ctrl.sentMessages()
	if _, ok := sends[0].msg.Payload.(*packets.TileSetEffect); !ok {
		t.Fatal("firmware effect was not stopped first")
	}
	if _, ok := sends[1].msg.Payload.(*packets.TileSet64); !ok {
		t.Fatal("colour did not follow firmware stop")
	}
	if result.Devices[0].FirmwareEffect.Running {
		t.Fatal("result still reports firmware animation")
	}
}

func TestThemeApplyPreflightLeavesAllDevicesAndEffectsUntouched(t *testing.T) {
	transport, ctrl, current := parameterTestTransport(t)
	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectBreathe}); err != nil {
		t.Fatal(err)
	}
	transport.mu.RLock()
	done := transport.effects[current.Serial].done
	transport.mu.RUnlock()
	bad := themeTestLight(t, "d073d501a2c4")
	bad.ProductID = 0
	ctrl.mu.Lock()
	ctrl.devices = append(ctrl.devices, bad)
	ctrl.mu.Unlock()
	if _, err := transport.ApplyTheme(context.Background(), testThemeRequest(ctrl.devices[0], bad)); err == nil {
		t.Fatal("invalid second target accepted")
	}
	transport.mu.RLock()
	if transport.effects[current.Serial].done != done {
		t.Error("failed preflight cancelled existing animation")
	}
	transport.mu.RUnlock()
	for _, send := range ctrl.sentMessages() {
		if send.serial == bad.Serial {
			t.Fatal("invalid target received controls")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	unlock, err := transport.lockDeviceCommand(ctx, ctrl.devices[0].Serial)
	if err != nil {
		t.Fatalf("failed preflight leaked target lock: %v", err)
	}
	unlock()
}

func TestThemeApplyReturnsPerDeviceFailuresAndContinues(t *testing.T) {
	a, b := themeTestLight(t, "d073d501a2c3"), themeTestLight(t, "d073d501a2c4")
	base := &fakeLifxController{devices: []lifxdevice.Device{b, a}}
	ctrl := &themeSendController{fakeLifxController: base, send: func(serial lifxdevice.Serial, msg *protocol.Message) error {
		if serial == a.Serial {
			return errors.New("send failed")
		}
		return base.Send(serial, msg)
	}}
	transport := newTestLifxTransport(t, ctrl)
	result, err := transport.ApplyTheme(context.Background(), testThemeRequest(b, a))
	if err != nil || len(result.Devices) != 1 || len(result.Failures) != 1 || result.Devices[0].Serial != b.Serial.String() || result.Failures[0].Serial != a.Serial.String() || !result.Failures[0].StateMayHaveChanged {
		t.Fatalf("partial result lost: %#v %v", result, err)
	}
	if transport.cachedDevice(a.Serial.String()) != nil || transport.cachedDevice(b.Serial.String()) == nil {
		t.Fatal("failed target cached as successful")
	}
	if math.Abs(result.Devices[0].Color.H-240) > .01 {
		t.Fatal("failure changed the other target's palette assignment")
	}
}

func TestThemeApplyCancellationPreservesSuccessfulTargets(t *testing.T) {
	a, b := themeTestLight(t, "d073d501a2c3"), themeTestLight(t, "d073d501a2c4")
	base := &fakeLifxController{devices: []lifxdevice.Device{a, b}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctrl := &themeSendController{fakeLifxController: base, send: func(serial lifxdevice.Serial, msg *protocol.Message) error { cancel(); return base.Send(serial, msg) }}
	transport := newTestLifxTransport(t, ctrl)
	result, err := transport.ApplyTheme(ctx, testThemeRequest(a, b))
	if err != nil || len(result.Devices) != 1 || len(result.Failures) != 1 || result.Failures[0].StateMayHaveChanged || len(base.sentMessages()) != 1 {
		t.Fatalf("cancellation lost partial results: %#v %v", result, err)
	}
}

func TestThemeApplyRejectsOfflineTargets(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := newTestLifxTransport(t, ctrl)
	current := mapLifxDevice(d, "desk")
	current.Online = false
	transport.storeCachedDevice(current)
	if _, err := transport.ApplyTheme(context.Background(), testThemeRequest(d)); err == nil {
		t.Fatal("offline target accepted")
	}
	if len(ctrl.sentMessages()) != 0 {
		t.Fatal("offline target received controls")
	}
}

func TestThemeGuardDoesNotUndoSubsequentLocalEdit(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := newTestLifxTransport(t, ctrl)
	result, err := transport.ApplyTheme(context.Background(), testThemeRequest(d))
	if err != nil {
		t.Fatal(err)
	}
	updated := result.Devices[0]
	updated.Color = &HSLColor{H: 90, S: .8, L: updated.Brightness, Kelvin: 3500}
	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: updated, Intent: DeviceCommandColor}); err != nil {
		t.Fatal(err)
	}
	snapshot := DeviceSnapshot{Devices: []Device{result.Devices[0]}}
	transport.reconcileRestoreSnapshot(&snapshot, time.Now())
	if !reflect.DeepEqual(snapshot.Devices[0], updated) {
		t.Fatal("theme guard undid a subsequent colour edit")
	}
}

func TestThemePowerFailureDoesNotClaimSuccessOrKeepOldGuard(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	d.PoweredOn = false
	base := &fakeLifxController{devices: []lifxdevice.Device{d}}
	ctrl := &themeSendController{fakeLifxController: base, send: func(serial lifxdevice.Serial, msg *protocol.Message) error {
		if _, ok := msg.Payload.(*packets.DeviceSetPower); ok {
			return errors.New("power send failed")
		}
		return base.Send(serial, msg)
	}}
	transport := newTestLifxTransport(t, ctrl)
	before := mapLifxDevice(d, "desk")
	transport.storeCachedDevice(before)
	transport.storeRestoreDevice(before, time.Now().Add(3*time.Second))
	result, err := transport.ApplyTheme(context.Background(), testThemeRequest(d))
	if err != nil || len(result.Devices) != 0 || len(result.Failures) != 1 || !result.Failures[0].StateMayHaveChanged || len(base.sentMessages()) != 1 {
		t.Fatalf("partial power failure: %#v %v", result, err)
	}
	if transport.cachedDevice(before.Serial) != nil || len(transport.restores) != 0 {
		t.Fatal("uncertain write kept an authoritative cached state")
	}
}

func TestThemeApplyPreservesEachStripZoneBrightness(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	d.LightType = lifxdevice.LightTypeMultiZone
	for _, brightness := range []float64{0, 30, 80, 50} {
		d.MultizoneProperties.Zones = append(d.MultizoneProperties.Zones, (lifxeffects.Color{Hue: 200, Saturation: 80, Brightness: brightness, Kelvin: 3500}).ToDeviceColor())
	}
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := newTestLifxTransport(t, ctrl)
	result, err := transport.ApplyTheme(context.Background(), testThemeRequest(d))
	if err != nil || len(result.Failures) != 0 {
		t.Fatalf("strip apply: %#v %v", result, err)
	}
	for i, color := range result.Devices[0].Zones {
		if math.Abs(color.L-lifxdevice.NewColor(d.MultizoneProperties.Zones[i]).Brightness/100) > .0001 {
			t.Fatalf("zone %d brightness changed", i)
		}
	}
}
