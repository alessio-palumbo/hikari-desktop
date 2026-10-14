package backend

import (
	"context"
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	maestrolive "github.com/alessio-palumbo/lifx-maestro/live"
	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

func musicLamp(t *testing.T, serial string) lifxdevice.Device {
	d := testLifxDevice(t, serial, "Lamp", "Home", "Desk")
	d.Type, d.LightType = lifxdevice.DeviceTypeLight, lifxdevice.LightTypeSingleZone
	d.ColorProperties.HasColor = true
	d.Color.Hue, d.Color.Saturation = 120, 80
	return d
}

func awaitMusicRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("music did not finish")
		return nil
	}
}

func startTestMusic(t *testing.T, transport *LifxTransport, serials []string) <-chan error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- transport.RunMusic(ctx, MusicInput{Serials: serials, Source: musicTestSource{}, Analyzer: &musicTestAnalyzer{}})
	}()
	return done
}

func TestMusicTransportPrimesOffLightAndRestoresRecentCachedState(t *testing.T) {
	d := musicLamp(t, "d073d501a2c3")
	d.PoweredOn = false
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}, sent: make(chan sentMessage, 32)}
	transport := newTestLifxTransport(t, ctrl)
	current := mapLifxDevice(d, "desk")
	current.Color = &HSLColor{H: 240, S: .8, L: .4, Kelvin: 4000}
	current.Brightness = .4
	transport.storeCachedDevice(current)
	done := startTestMusic(t, transport, []string{d.Serial.String(), d.Serial.String()})
	first := waitForSentMessage(t, ctrl.sent)
	if _, ok := first.msg.Payload.(*packets.LightSetWaveformOptional); !ok {
		t.Fatalf("first message=%T, want color before power", first.msg.Payload)
	}
	second := waitForSentMessage(t, ctrl.sent)
	if power, ok := second.msg.Payload.(*packets.DeviceSetPower); !ok || power.Level != 65535 {
		t.Fatalf("second=%+v", second.msg.Payload)
	}
	if err := transport.StopMusic(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := awaitMusicRun(t, done); err != nil {
		t.Fatal(err)
	}
	restored := restoredDeviceState(t, ctrl)
	if restored.PoweredOn || restored.Color.Hue < 239 || restored.Color.Hue > 241 || restored.Color.Brightness != 40 {
		t.Fatalf("restored stale state: %+v", restored)
	}
	sends := ctrl.sentMessages()
	if power, ok := sends[len(sends)-1].msg.Payload.(*packets.DeviceSetPower); !ok || power.Level != 0 {
		t.Fatal("missing power-off before restore")
	}
}

func TestMusicTransportManualChangeStopsGroupButUnrelatedControlDoesNot(t *testing.T) {
	a, b, other := musicLamp(t, "d073d501a2c3"), musicLamp(t, "d073d501a2c4"), musicLamp(t, "d073d501a2c5")
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{a, b, other}, sent: make(chan sentMessage, 32)}
	transport := newTestLifxTransport(t, ctrl)
	done := startTestMusic(t, transport, []string{a.Serial.String(), b.Serial.String()})
	waitForSentMessage(t, ctrl.sent)
	waitForSentMessage(t, ctrl.sent)
	if !transport.MusicOwnsSerial(a.Serial.String()) || transport.MusicOwnsSerial(other.Serial.String()) {
		t.Fatal("incorrect target ownership")
	}
	request := mapLifxDevice(other, "desk")
	request.On = false
	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: request, Intent: DeviceCommandPower}); err != nil {
		t.Fatal(err)
	}
	if transport.MusicStatus().State != "running" || len(ctrl.restoredStateSnapshots()) != 0 {
		t.Fatal("unrelated command stopped music")
	}
	if _, _, err := transport.CaptureSceneState(context.Background(), []string{a.Serial.String()}); err == nil {
		t.Fatal("captured animation as a scene")
	}
	if err := transport.CheckSetupReady(); err == nil {
		t.Fatal("setup allowed during music")
	}
	request = mapLifxDevice(a, "desk")
	request.On = false
	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: request, Intent: DeviceCommandPower}); err != nil {
		t.Fatal(err)
	}
	if err := awaitMusicRun(t, done); err != nil {
		t.Fatal(err)
	}
	if transport.MusicStatus().State != "stopped" || len(ctrl.restoredStateSnapshots()) != 2 {
		t.Fatalf("status=%+v restores=%d", transport.MusicStatus(), len(ctrl.restoredStateSnapshots()))
	}
}

func TestPresenceCommandDoesNotStopOrWriteMusicTargets(t *testing.T) {
	d := musicLamp(t, "d073d501a2c3")
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}, sent: make(chan sentMessage, 32)}
	transport := newTestLifxTransport(t, ctrl)
	done := startTestMusic(t, transport, []string{d.Serial.String()})
	waitForSentMessage(t, ctrl.sent)
	request := mapLifxDevice(d, "desk")
	request.On = false
	count := len(ctrl.sentMessages())
	result, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: request, Intent: DeviceCommandPower, Automation: true})
	if err != nil || !result.On || len(ctrl.sentMessages()) != count || transport.MusicStatus().State != "running" {
		t.Fatalf("automation competed with Music: result=%+v err=%v", result, err)
	}
	if err := transport.StopMusic(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := awaitMusicRun(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestMusicTransportCapturesEveryTargetBeforeAnySend(t *testing.T) {
	a, b := musicLamp(t, "d073d501a2c3"), musicLamp(t, "d073d501a2c4")
	base := &fakeLifxController{devices: []lifxdevice.Device{a, b}}
	failure := errors.New("missing observed state")
	ctrl := &snapshotBoundaryController{fakeLifxController: base, capture: func(ctx context.Context, serials []lifxdevice.Serial, opts lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
		if serials[0] == b.Serial {
			return lifxdevice.StateSnapshot{}, failure
		}
		return base.CaptureStateSnapshot(ctx, serials, opts)
	}}
	transport := newTestLifxTransport(t, ctrl)
	analyzer := &musicTestAnalyzer{}
	err := transport.RunMusic(context.Background(), MusicInput{Serials: []string{a.Serial.String(), b.Serial.String()}, Source: musicTestSource{}, Analyzer: analyzer})
	if !errors.Is(err, failure) || len(base.sentMessages()) != 0 || len(base.restoredStateSnapshots()) != 0 || analyzer.closes.Load() != 1 {
		t.Fatalf("capture failure=%v", err)
	}
	unlock, err := transport.lockDeviceCommand(context.Background(), a.Serial)
	if err != nil {
		t.Fatal(err)
	}
	unlock()
}

func TestMusicTransportStopDuringCaptureDoesNotPrimeOrPowerOn(t *testing.T) {
	d := musicLamp(t, "d073d501a2c3")
	d.PoweredOn = false
	base := &fakeLifxController{devices: []lifxdevice.Device{d}}
	entered := make(chan struct{})
	ctrl := &snapshotBoundaryController{fakeLifxController: base, capture: func(ctx context.Context, _ []lifxdevice.Serial, _ lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
		close(entered)
		<-ctx.Done()
		return lifxdevice.StateSnapshot{}, ctx.Err()
	}}
	transport := newTestLifxTransport(t, ctrl)
	done := startTestMusic(t, transport, []string{d.Serial.String()})
	awaitMusic(t, entered)
	if err := transport.StopMusic(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := awaitMusicRun(t, done); err != nil {
		t.Fatal(err)
	}
	if len(base.sentMessages()) != 0 || len(base.restoredStateSnapshots()) != 0 {
		t.Fatal("cancelled preparation modified the light")
	}
}

func TestMusicTransportRendersAndRestoresBothOrientedTiles(t *testing.T) {
	d := previewTestDevice(55, 8, 8, 2)
	d.Serial, _ = lifxdevice.SerialFromHex("d073d501a2c3")
	d.PoweredOn = true
	d.Color = lifxdevice.Color{Hue: 120, Saturation: 80, Brightness: 50, Kelvin: 3500}
	d.MatrixProperties.ChainOrientations = []lifxdevice.Orientation{lifxdevice.OrientationRight, lifxdevice.OrientationLeft}
	d.MatrixProperties.ChainZones = [][]packets.LightHsbk{make([]packets.LightHsbk, 64), make([]packets.LightHsbk, 64)}
	for chain := range d.MatrixProperties.ChainZones {
		for i := range d.MatrixProperties.ChainZones[chain] {
			d.MatrixProperties.ChainZones[chain][i] = testHSBK(float64(20 + chain*150 + i))
		}
	}
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}, sent: make(chan sentMessage, 32)}
	transport := newTestLifxTransport(t, ctrl)
	done := startTestMusic(t, transport, []string{d.Serial.String()})
	a, b := waitForTileSet64(t, ctrl.sent), waitForTileSet64(t, ctrl.sent)
	if a.TileIndex != 0 || b.TileIndex != 1 || a.Length != 1 || b.Length != 1 {
		t.Fatalf("incorrect chain mapping: %+v %+v", a, b)
	}
	if err := transport.StopMusic(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := awaitMusicRun(t, done); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restoredDeviceState(t, ctrl).MatrixChains, d.MatrixProperties.ChainZones) {
		t.Fatal("restoration lost tile pixels or orientation")
	}
}

func TestMusicTransportDoesNotRestoreIntoChangedEndpoint(t *testing.T) {
	d := musicLamp(t, "d073d501a2c3")
	d.Address = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 56700}
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}, sent: make(chan sentMessage, 32)}
	transport := newTestLifxTransport(t, ctrl)
	done := startTestMusic(t, transport, []string{d.Serial.String()})
	waitForSentMessage(t, ctrl.sent)
	ctrl.mu.Lock()
	ctrl.devices[0].Address = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 2), Port: 56700}
	ctrl.mu.Unlock()
	if err := transport.StopMusic(context.Background()); !errors.Is(err, lifxcontroller.ErrEndpointChanged) {
		t.Fatalf("endpoint=%v", err)
	}
	if err := awaitMusicRun(t, done); !errors.Is(err, lifxcontroller.ErrEndpointChanged) {
		t.Fatalf("run=%v", err)
	}
	if len(ctrl.restoredStateSnapshots()) != 0 {
		t.Fatal("restored stale endpoint state")
	}
}

type failingMusicController struct{ *fakeLifxController }

func (c *failingMusicController) Send(serial lifxdevice.Serial, msg *protocol.Message) error {
	if _, ok := msg.Payload.(*packets.LightSetWaveformOptional); ok {
		return errors.New("frame send failed")
	}
	return c.fakeLifxController.Send(serial, msg)
}

func TestMusicTransportRestoresAfterPossiblePartialSend(t *testing.T) {
	d := musicLamp(t, "d073d501a2c3")
	base := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := newTestLifxTransport(t, &failingMusicController{base})
	err := transport.RunMusic(context.Background(), MusicInput{Serials: []string{d.Serial.String()}, Source: musicTestSource{}, Analyzer: &musicTestAnalyzer{}})
	if err == nil || len(base.restoredStateSnapshots()) != 1 || transport.MusicStatus().State != "error" {
		t.Fatalf("failure=%v status=%+v", err, transport.MusicStatus())
	}
}

func TestMusicFramesRespectWhiteOnlyCapability(t *testing.T) {
	frame := lifxeffects.NewFrame(1, 1, time.Millisecond, lifxeffects.Color{Hue: 200, Saturation: 90, Brightness: 40, Kelvin: 9000})
	got := constrainMusicFrame(frame, DeviceCapability{KelvinMin: 2700, KelvinMax: 2700})
	if got.Colors[0].Saturation != 0 || got.Colors[0].Kelvin != 2700 || got.Colors[0].Brightness != 40 || frame.Colors[0].Saturation != 90 {
		t.Fatalf("constrained=%+v original=%+v", got, frame)
	}
}

func TestManualChangeDoesNotKeepUnrelatedAnimatedState(t *testing.T) {
	restored := Device{On: false, Brightness: .4, Kelvin: 3500, Color: &HSLColor{H: 120, S: .8, L: .4}, Chain: []Matrix{{Pixels: []HSLColor{{H: 120, S: .8, L: .4}}}}}
	request := Device{On: true, Brightness: .9, Kelvin: 4500, Color: &HSLColor{H: 240, S: .5, L: .9}}
	power := rebaseMusicDeviceChange(request, restored, DeviceCommandPower)
	if !power.On || power.Brightness != .4 || power.Color.H != 120 {
		t.Fatalf("power kept animation: %+v", power)
	}
	brightness := rebaseMusicDeviceChange(request, restored, DeviceCommandBrightness)
	if brightness.Color.H != 120 || brightness.Color.L != .9 || brightness.Chain[0].Pixels[0].L != .9 {
		t.Fatalf("brightness overwrote restored pattern: %+v", brightness)
	}
	color := rebaseMusicDeviceChange(request, restored, DeviceCommandColor)
	if color.Color.H != 240 || color.Color.L != .4 || color.Brightness != .4 {
		t.Fatalf("color kept animated brightness: %+v", color)
	}
	if restored.Chain[0].Pixels[0].L != .4 {
		t.Fatal("request mutated restore state")
	}
}

var _ maestrolive.AudioSource = musicTestSource{}
