package backend

import (
	"context"
	"errors"
	"net"
	"reflect"
	"strings"
	"testing"

	lifxclient "github.com/alessio-palumbo/lifxlan-go/pkg/client"
	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

type fakePartController struct {
	*fakeLifxController
	states     map[lifxdevice.LightPart]lifxcontroller.LightPartState
	part       lifxdevice.LightPart
	color      *lifxcontroller.LightStateUpdate
	power      *bool
	powerOpts  lifxcontroller.LightPartPowerOptions
	err        error
	powerErr   error
	stateErr   error
	operations []string
	refreshes  int
	refreshErr error
	colorSends int
	powerSends int
	freshReads int
}

func (f *fakePartController) GetLightPartState(_ lifxdevice.Serial, part lifxdevice.LightPart) (lifxcontroller.LightPartState, error) {
	return f.states[part], f.stateErr
}

func (f *fakePartController) SetLightPartColor(ctx context.Context, _ lifxdevice.Serial, part lifxdevice.LightPart, update lifxcontroller.LightStateUpdate, _ lifxcontroller.SnapshotOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.part, f.color = part, &update
	f.colorSends++
	f.operations = append(f.operations, "color")
	return f.err
}

func (f *fakePartController) SetLightPartPower(ctx context.Context, _ lifxdevice.Serial, part lifxdevice.LightPart, on bool, opts lifxcontroller.LightPartPowerOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.part, f.power, f.powerOpts = part, &on, opts
	f.powerSends++
	f.operations = append(f.operations, "power")
	if f.powerErr != nil {
		return f.powerErr
	}
	return f.err
}

func (f *fakePartController) RefreshDiscovery() error {
	f.refreshes++
	return f.refreshErr
}

func (f *fakePartController) CaptureStateSnapshot(ctx context.Context, serials []lifxdevice.Serial, opts lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
	if opts.RequireFresh {
		f.freshReads++
	}
	return f.fakeLifxController.CaptureStateSnapshot(ctx, serials, opts)
}

func newPartTestController(t *testing.T) (*fakePartController, lifxdevice.Device) {
	t.Helper()
	d := testLifxDevice(t, "d073d501a2c3", "Ceiling", "Home", "Desk")
	d.SetProductInfo(265)
	d.MatrixProperties = lifxdevice.MatrixProperties{Width: 8, Height: 8, NZones: 64, ChainLength: 1}
	return &fakePartController{fakeLifxController: &fakeLifxController{devices: []lifxdevice.Device{d}}, states: map[lifxdevice.LightPart]lifxcontroller.LightPartState{}}, d
}

func TestLightPartsKeepOneIdentityAndKnownObservedState(t *testing.T) {
	ctrl, d := newPartTestController(t)
	ctrl.states[lifxdevice.LightPartMain] = lifxcontroller.LightPartState{Known: true, On: false, Brightness: 70, DevicePoweredOn: false}
	ctrl.states[lifxdevice.LightPartUplight] = lifxcontroller.LightPartState{Known: false}
	tpt := newTestLifxTransport(t, ctrl)
	snapshot, err := tpt.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].Serial != d.Serial.String() {
		t.Fatal("parts duplicated the device identity")
	}
	got := snapshot.Devices[0]
	if len(got.LightParts) != 2 || got.LightParts[0].ZoneCount != 56 || got.LightParts[1].ZoneCount != 1 || got.PixelCount != 56 {
		t.Fatalf("part metadata=%#v", got.LightParts)
	}
	main := got.LightPartStates[lifxdevice.LightPartMain]
	if !main.Known || main.On || main.DevicePoweredOn || main.Brightness != 0.7 || got.LightPartStates[lifxdevice.LightPartUplight].Known {
		t.Fatalf("part observations=%#v", got.LightPartStates)
	}
}

func TestLightPartColorAndPowerStaySeparate(t *testing.T) {
	for _, part := range []lifxdevice.LightPart{lifxdevice.LightPartMain, lifxdevice.LightPartUplight, lifxdevice.LightPartAll} {
		t.Run(string(part), func(t *testing.T) {
			ctrl, d := newPartTestController(t)
			tpt := newTestLifxTransport(t, ctrl)
			zero := 0.0
			if err := tpt.SetLightPartState(context.Background(), SetLightPartStateRequest{Serial: d.Serial.String(), Part: part, Brightness: &zero}); err != nil {
				t.Fatal(err)
			}
			if ctrl.part != part || ctrl.color.Brightness == nil || *ctrl.color.Brightness != 0 || ctrl.color.Power != nil || ctrl.color.Hue != nil || ctrl.color.Saturation != nil || ctrl.powerSends != 0 {
				t.Fatal("zero brightness became power intent or replaced color")
			}
			on := true
			if err := tpt.SetLightPartState(context.Background(), SetLightPartStateRequest{Serial: d.Serial.String(), Part: part, Power: &on}); err != nil {
				t.Fatal(err)
			}
			if ctrl.power == nil || !*ctrl.power || ctrl.powerOpts.FallbackBrightness == nil || *ctrl.powerOpts.FallbackBrightness != 1 || ctrl.colorSends != 1 || len(ctrl.sentMessages()) != 0 {
				t.Fatal("power inheritance/sequencing was implemented locally")
			}
		})
	}
}

func TestLightPartWhiteAndPartialErrors(t *testing.T) {
	ctrl, d := newPartTestController(t)
	ctrl.states[lifxdevice.LightPartUplight] = lifxcontroller.LightPartState{Known: true, On: true}
	tpt := newTestLifxTransport(t, ctrl)
	zero, kelvin := 0.0, 3500
	req := SetLightPartStateRequest{Serial: d.Serial.String(), Part: lifxdevice.LightPartUplight, Saturation: &zero, Kelvin: &kelvin}
	if err := tpt.SetLightPartState(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if ctrl.color.Brightness != nil || ctrl.color.Hue != nil || *ctrl.color.Kelvin != 3500 || *ctrl.color.Saturation != 0 {
		t.Fatal("white update changed unrelated HSBK fields")
	}
	ctrl.err = &lifxcontroller.StateSendError{Sent: 1, Total: 2, Err: lifxcontroller.ErrEndpointChanged}
	err := tpt.SetLightPartState(context.Background(), req)
	if !errors.Is(err, lifxcontroller.ErrEndpointChanged) || !strings.Contains(err.Error(), "partially updated") || ctrl.colorSends != 2 {
		t.Fatalf("error not propagated without retry: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tpt.SetLightPartState(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
}

func TestPartColorSelectionPreparesColorsBeforeActivation(t *testing.T) {
	for _, part := range []lifxdevice.LightPart{lifxdevice.LightPartMain, lifxdevice.LightPartUplight, lifxdevice.LightPartAll} {
		for _, white := range []bool{false, true} {
			name := string(part) + "/color"
			if white {
				name = string(part) + "/white"
			}
			t.Run(name, func(t *testing.T) {
				ctrl, d := newPartTestController(t)
				// Both parts can have zero retained brightness. Hikari supplies
				// only an activation fallback; the library plans inherited state.
				ctrl.states[part] = lifxcontroller.LightPartState{Known: true, On: false, Brightness: 0, DevicePoweredOn: false}
				req := SetLightPartStateRequest{Serial: d.Serial.String(), Part: part}
				hue, saturation, kelvin := 275.0, 80.0, 3500
				if white {
					saturation = 0
					req.Saturation, req.Kelvin = &saturation, &kelvin
				} else {
					req.Hue, req.Saturation = &hue, &saturation
				}
				if err := newTestLifxTransport(t, ctrl).SetLightPartState(context.Background(), req); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(ctrl.operations, []string{"color", "power"}) || ctrl.part != part || ctrl.power == nil || !*ctrl.power {
					t.Fatalf("activation order=%v", ctrl.operations)
				}
				if ctrl.color.Brightness != nil || ctrl.color.Power != nil || ctrl.color.Duration != 0 || ctrl.powerOpts.FallbackBrightness == nil || *ctrl.powerOpts.FallbackBrightness != 1 {
					t.Fatal("activation changed brightness locally or sampled a color transition")
				}
				if len(ctrl.sentMessages()) != 0 {
					t.Fatal("Hikari bypassed the library's part-power sequencing")
				}
			})
		}
	}
}

func TestPartColorSelectionDoesNotPowerAlreadyActivePart(t *testing.T) {
	ctrl, d := newPartTestController(t)
	ctrl.states[lifxdevice.LightPartMain] = lifxcontroller.LightPartState{Known: true, On: true, Brightness: 70, DevicePoweredOn: true}
	hue := 45.0
	if err := newTestLifxTransport(t, ctrl).SetLightPartState(context.Background(), SetLightPartStateRequest{Serial: d.Serial.String(), Part: lifxdevice.LightPartMain, Hue: &hue}); err != nil {
		t.Fatal(err)
	}
	if ctrl.powerSends != 0 || ctrl.color.Brightness != nil || ctrl.color.Duration != defaultColorTransitionDuration {
		t.Fatal("active color selection changed power, brightness, or transition duration")
	}
}

func TestPositivePartBrightnessActivatesAfterBrightnessSend(t *testing.T) {
	for _, part := range []lifxdevice.LightPart{lifxdevice.LightPartMain, lifxdevice.LightPartUplight, lifxdevice.LightPartAll} {
		t.Run(string(part), func(t *testing.T) {
			ctrl, d := newPartTestController(t)
			ctrl.states[part] = lifxcontroller.LightPartState{Known: true, On: false, DevicePoweredOn: false}
			brightness := 0.4
			if err := newTestLifxTransport(t, ctrl).SetLightPartState(context.Background(), SetLightPartStateRequest{Serial: d.Serial.String(), Part: part, Brightness: &brightness}); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ctrl.operations, []string{"color", "power"}) || ctrl.color.Brightness == nil || *ctrl.color.Brightness != 40 || ctrl.color.Hue != nil || ctrl.color.Saturation != nil || ctrl.color.Kelvin != nil || ctrl.color.Duration != 0 || ctrl.power == nil || !*ctrl.power {
				t.Fatal("brightness did not prepare state before activation, or changed color")
			}
		})
	}
}

func TestPositivePartBrightnessDoesNotPowerAlreadyActivePart(t *testing.T) {
	ctrl, d := newPartTestController(t)
	ctrl.states[lifxdevice.LightPartMain] = lifxcontroller.LightPartState{Known: true, On: true, DevicePoweredOn: true}
	brightness := 0.4
	if err := newTestLifxTransport(t, ctrl).SetLightPartState(context.Background(), SetLightPartStateRequest{Serial: d.Serial.String(), Part: lifxdevice.LightPartMain, Brightness: &brightness}); err != nil {
		t.Fatal(err)
	}
	if ctrl.powerSends != 0 || *ctrl.color.Brightness != 40 || ctrl.color.Duration != defaultColorTransitionDuration {
		t.Fatal("active brightness change sent redundant power")
	}
}

func TestPartActivationPreservesLibraryInheritanceAndOffHasNoFallback(t *testing.T) {
	ctrl, d := newPartTestController(t)
	tpt := newTestLifxTransport(t, ctrl)
	ctrl.states[lifxdevice.LightPartUplight] = lifxcontroller.LightPartState{Known: true, On: false, Brightness: 0, DevicePoweredOn: true}
	ctrl.states[lifxdevice.LightPartMain] = lifxcontroller.LightPartState{Known: true, On: true, Brightness: 75, DevicePoweredOn: true}
	for _, on := range []bool{true, false} {
		if err := tpt.SetLightPartState(context.Background(), SetLightPartStateRequest{Serial: d.Serial.String(), Part: lifxdevice.LightPartUplight, Power: &on}); err != nil {
			t.Fatal(err)
		}
		if on && (ctrl.powerOpts.FallbackBrightness == nil || *ctrl.powerOpts.FallbackBrightness != 1) || !on && ctrl.powerOpts.FallbackBrightness != nil {
			t.Fatal("fallback policy changed")
		}
	}
	if ctrl.colorSends != 0 {
		t.Fatal("Hikari calculated or set inherited brightness instead of delegating it")
	}
}

func TestPartColorActivationErrorsDoNotRetryOrContinueFailedWrites(t *testing.T) {
	for _, phase := range []string{"observe", "color", "power", "unknown"} {
		t.Run(phase, func(t *testing.T) {
			ctrl, d := newPartTestController(t)
			ctrl.states[lifxdevice.LightPartUplight] = lifxcontroller.LightPartState{Known: true, On: false}
			switch phase {
			case "observe":
				ctrl.stateErr = lifxcontroller.ErrEndpointChanged
			case "color":
				ctrl.err = &lifxcontroller.StateSendError{Sent: 1, Total: 2, Err: lifxcontroller.ErrEndpointChanged}
			case "power":
				ctrl.powerErr = lifxcontroller.ErrEndpointChanged
			case "unknown":
				ctrl.states[lifxdevice.LightPartUplight] = lifxcontroller.LightPartState{Known: false}
			}
			hue := 45.0
			err := newTestLifxTransport(t, ctrl).SetLightPartState(context.Background(), SetLightPartStateRequest{Serial: d.Serial.String(), Part: lifxdevice.LightPartUplight, Hue: &hue})
			if err == nil {
				t.Fatal("failure was hidden")
			}
			if phase != "unknown" && !errors.Is(err, lifxcontroller.ErrEndpointChanged) {
				t.Fatalf("underlying error lost: %v", err)
			}
			if phase == "power" {
				if ctrl.colorSends != 1 || ctrl.powerSends != 1 || !strings.Contains(err.Error(), "partially updated") {
					t.Fatalf("partial failure=%v, sends=%v", err, ctrl.operations)
				}
			} else if ctrl.powerSends != 0 || ctrl.colorSends > 1 {
				t.Fatal("failed color request was retried or activated")
			}
			if (phase == "observe" || phase == "unknown") && ctrl.colorSends != 0 {
				t.Fatal("unknown state was used for dependent control")
			}
		})
	}
}

func TestUplightGlobalControlsUsePartAPI(t *testing.T) {
	ctrl, d := newPartTestController(t)
	tpt := newTestLifxTransport(t, ctrl)
	requested := mapLifxDevice(d, "desk")
	requested.Brightness = 0
	if _, err := tpt.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: requested, Intent: DeviceCommandBrightness}); err != nil {
		t.Fatal(err)
	}
	if ctrl.part != lifxdevice.LightPartAll || ctrl.powerSends != 0 || *ctrl.color.Brightness != 0 {
		t.Fatal("global brightness used local power sequencing")
	}
}

func TestEffectivePartOffDoesNotChangeSharedPowerInEffectSnapshot(t *testing.T) {
	d := Device{Kind: DeviceKindSingle, On: false, LightPartStates: map[lifxdevice.LightPart]LightPartState{
		lifxdevice.LightPartAll: {Known: true, On: false, Brightness: 0, DevicePoweredOn: true},
	}}
	state := lifxdevice.DeviceStateSnapshot{}
	overlayAppEffectState(&state, d)
	if !state.PoweredOn || !devicePoweredOn(d) {
		t.Fatal("effective part-off overwrote shared device power")
	}
}

func TestSameInterfaceDiscoveryRefreshKeepsControllerAndSessions(t *testing.T) {
	ctrl, _ := newPartTestController(t)
	tpt := newTestLifxTransport(t, ctrl)
	if _, err := tpt.RestartDeviceDiscovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	if ctrl.refreshes != 1 || ctrl.closed || tpt.currentController() != ctrl {
		t.Fatal("same-interface refresh recreated the controller")
	}
}

func TestNetworkReturnRefreshesExistingController(t *testing.T) {
	ctrl, _ := newPartTestController(t)
	tpt := newTestLifxTransport(t, ctrl)
	tpt.networkNeedsRefresh = true
	if err := tpt.ensureNetworkInterfaceAvailable(); err != nil {
		t.Fatal(err)
	}
	if err := tpt.ensureNetworkInterfaceAvailable(); err != nil {
		t.Fatal(err)
	}
	if ctrl.refreshes != 1 || ctrl.closed {
		t.Fatal("network recovery did not refresh once without recreating")
	}
}

func TestDiscoveryRefreshDoesNotRecreateForTemporaryNetworkFailure(t *testing.T) {
	ctrl, _ := newPartTestController(t)
	ctrl.refreshErr = errors.New("selected interface unavailable")
	tpt := newTestLifxTransport(t, ctrl)
	if _, err := tpt.RestartDeviceDiscovery(context.Background()); !errors.Is(err, ctrl.refreshErr) {
		t.Fatalf("error=%v", err)
	}
	if ctrl.closed || tpt.currentController() != ctrl {
		t.Fatal("temporary network failure replaced socket")
	}
}

func TestDiscoveryRefreshRecreatesForClosedSocket(t *testing.T) {
	ctrl, _ := newPartTestController(t)
	ctrl.refreshErr = net.ErrClosed
	tpt := newTestLifxTransport(t, ctrl)
	replacement := &fakePartController{fakeLifxController: &fakeLifxController{}}
	tpt.controllerFactory = func(*lifxclient.Config) (lifxController, error) { return replacement, nil }
	if _, err := tpt.RestartDeviceDiscovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tpt.Close(context.Background()) })
	if !ctrl.closed || tpt.currentController() != replacement {
		t.Fatal("closed socket was not replaced")
	}
}

func TestAddressChangeInvalidatesDependentStateWithoutRemovingDevice(t *testing.T) {
	ctrl, d := newPartTestController(t)
	tpt := newTestLifxTransport(t, ctrl)
	tpt.observedReady = true
	tpt.storeCachedDevice(mapLifxDevice(d, "desk"))
	tpt.restores[d.Serial.String()] = effectRestore{device: mapLifxDevice(d, "desk")}
	d.Address = &net.UDPAddr{IP: net.IPv4(192, 168, 1, 42), Port: 56701}
	ctrl.setDevices([]lifxdevice.Device{d})
	tpt.storeObservedDevice(0, lifxcontroller.DeviceEvent{Type: lifxcontroller.DeviceEventUpdated, Revision: 1, Device: d, Changes: lifxcontroller.DeviceChangeAddress})
	if len(tpt.observed) != 1 || tpt.cachedDevice(d.Serial.String()) != nil || len(tpt.restores) != 0 {
		t.Fatal("address move retained dependent state or removed identity")
	}
	snapshot, err := tpt.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Devices[0].IPAddress != "192.168.1.42" || snapshot.Devices[0].EndpointRevision != 1 || snapshot.Devices[0].LightPartStates[lifxdevice.LightPartMain].Known {
		t.Fatalf("moved snapshot=%#v", snapshot.Devices[0])
	}
	if err := tpt.refreshEndpointState(context.Background(), ctrl, d.Serial); err != nil {
		t.Fatal(err)
	}
	if ctrl.freshReads != 1 || tpt.endpointNeedsState[d.Serial.String()] {
		t.Fatal("dependent state was not refreshed")
	}
	stale := mapLifxDevice(d, "desk")
	tpt.storeCachedDevice(stale)
	if cached := tpt.cachedDevice(d.Serial.String()); cached != nil && cached.EndpointRevision == 0 {
		t.Fatal("stale completion overwrote endpoint revision")
	}
	_, err = tpt.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: stale, Intent: DeviceCommandPower})
	if !errors.Is(err, lifxcontroller.ErrEndpointChanged) || ctrl.powerSends != 0 {
		t.Fatalf("stale request sent controls: %v", err)
	}
}

func TestResyncDetectsMissedAddressMoveIncludingPortOnlyChanges(t *testing.T) {
	ctrl, d := newPartTestController(t)
	tpt := newTestLifxTransport(t, ctrl)
	d.Address = &net.UDPAddr{IP: net.IPv4(192, 168, 1, 42), Port: 56700}
	tpt.observed[d.Serial] = d.Clone()
	d.Address.Port = 56701
	ctrl.setDevices([]lifxdevice.Device{d})
	if changed, ready := tpt.resyncObservedDevices(ctrl, 0, 1); !changed || !ready {
		t.Fatal("resync failed")
	}
	if tpt.endpointRevision[d.Serial.String()] != 1 || !tpt.endpointNeedsState[d.Serial.String()] || len(tpt.observed) != 1 {
		t.Fatal("resync missed changed endpoint or removed identity")
	}
}
