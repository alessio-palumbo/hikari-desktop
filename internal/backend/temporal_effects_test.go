package backend

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	lifxadapters "github.com/alessio-palumbo/lifxlan-go/pkg/effects/adapters"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

func TestBreathePreservesPatternAndOriginalBrightness(t *testing.T) {
	d := lifxdevice.Device{LightType: lifxdevice.LightTypeMultiZone}
	d.MultizoneProperties.Zones = []packets.LightHsbk{
		(lifxeffects.Color{Hue: 20, Saturation: 80, Brightness: 80, Kelvin: 3500}).ToDeviceColor(),
		(lifxeffects.Color{Hue: 220, Saturation: 60, Brightness: 40, Kelvin: 4500}).ToDeviceColor(),
		(lifxeffects.Color{Hue: 100, Saturation: 40, Kelvin: 3000}).ToDeviceColor(),
	}
	initial, err := lifxeffects.FrameFromDeviceState(d, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	effect, err := newPatternBreathe(StartDeviceEffectRequest{SpeedMS: 4000}, d)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		dt     time.Duration
		factor float64
	}{
		{0, .1}, {2 * time.Second, 1}, {2 * time.Second, .1}, {2 * time.Second, 1},
	} {
		frame, _ := effect.Next(sample.dt)
		for i, got := range frame.Colors {
			want := initial.Colors[i]
			if got.Hue != want.Hue || got.Saturation != want.Saturation || got.Kelvin != want.Kelvin || math.Abs(got.Brightness-want.Brightness*sample.factor) > .001 {
				t.Fatalf("cell %d: %#v, initial %#v, factor %v", i, got, want, sample.factor)
			}
		}
	}
}

func TestBreatheRoundTripsOrientedTileChain(t *testing.T) {
	d := previewTestDevice(55, 8, 8, 2)
	d.MatrixProperties.ChainOrientations = []lifxdevice.Orientation{lifxdevice.OrientationRight, lifxdevice.OrientationLeft}
	d.MatrixProperties.ChainZones = make([][]packets.LightHsbk, 2)
	for chain := range d.MatrixProperties.ChainZones {
		d.MatrixProperties.ChainZones[chain] = make([]packets.LightHsbk, 64)
		for i := range d.MatrixProperties.ChainZones[chain] {
			d.MatrixProperties.ChainZones[chain][i] = (lifxeffects.Color{Hue: float64((i*13 + chain*170) % 360), Saturation: 90, Brightness: 50, Kelvin: 3500}).ToDeviceColor()
		}
	}
	effect, err := newPatternBreathe(StartDeviceEffectRequest{SpeedMS: 4000}, d)
	if err != nil {
		t.Fatal(err)
	}
	frame, _ := effect.Next(2 * time.Second)
	surface := lifxdevice.SurfaceFromDevice(d)
	state := lifxeffects.NewPhysicalColorState(surface)
	renderer := lifxadapters.NewRendererForDevice(d, func(msg *protocol.Message) error { return applyPreviewPacket(&state, surface, msg) })
	if err := renderer.RenderFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	for chain := range state.MatrixChains {
		for i, got := range state.MatrixChains[chain] {
			want := d.MatrixProperties.ChainZones[chain][i]
			if got.Hue != want.Hue || got.Saturation != want.Saturation || got.Kelvin != want.Kelvin || math.Abs(float64(got.Brightness)-float64(want.Brightness)) > 1 {
				t.Fatalf("chain %d cell %d changed orientation or colour: %#v want %#v", chain, i, got, want)
			}
		}
	}
}

func TestColorCyclePreservesBrightnessAndIsUniform(t *testing.T) {
	for _, brightness := range []float64{0, .35, 1} {
		current := Device{Kind: DeviceKindMultizone, Brightness: brightness, Color: &HSLColor{H: 210, S: .8, L: brightness, Kelvin: 3500}, Capability: DeviceCapability{HasColor: true}}
		d := lifxdevice.Device{LightType: lifxdevice.LightTypeMultiZone, MultizoneProperties: lifxdevice.MultizoneProperties{Zones: make([]packets.LightHsbk, 8)}}
		effect, err := newAppEffect(StartDeviceEffectRequest{Effect: DeviceEffectColorCycle, SpeedMS: 8000}, d, current)
		if err != nil {
			t.Fatal(err)
		}
		first, _ := effect.Next(0)
		other, _ := effect.Next(2 * time.Second)
		for _, frame := range []lifxeffects.Frame{first, other} {
			for _, color := range frame.Colors {
				if math.Abs(color.Brightness-brightness*100) > .001 || color != frame.Colors[0] {
					t.Fatalf("nonuniform or raised brightness: %#v", frame)
				}
			}
		}
		if first.Colors[0].Hue == other.Colors[0].Hue {
			t.Fatal("static input did not produce a colour cycle")
		}
		effect.Reset()
		fullCycle, _ := effect.Next(8 * time.Second)
		for i := range first.Colors {
			if math.Abs(first.Colors[i].Hue-fullCycle.Colors[i].Hue) > .00001 {
				t.Fatal("speed is not the full cycle period")
			}
		}
	}
}

func TestTemporalEffectsUseCacheAndRestoreSingleZone(t *testing.T) {
	for _, effect := range []DeviceEffect{DeviceEffectBreathe, DeviceEffectColorCycle} {
		for _, on := range []bool{true, false} {
			t.Run(string(effect)+map[bool]string{true: "/on", false: "/off"}[on], func(t *testing.T) {
				d := testLifxDevice(t, "d073d501a2c3", "Lamp", "Home", "Desk")
				d.LightType = lifxdevice.LightTypeSingleZone
				d.Color = lifxdevice.Color{Hue: 120, Saturation: 90, Brightness: 80, Kelvin: 3500}
				ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}, sent: make(chan sentMessage, 32)}
				transport := newTestLifxTransport(t, ctrl)
				stale := mapLifxDevice(d, "desk")
				current := stale
				current.On, current.Brightness = on, .4
				current.Color = &HSLColor{H: 240, S: .8, L: .4, Kelvin: 4000}
				transport.storeCachedDevice(current)
				status, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: stale, Effect: effect, SpeedMS: 4000})
				if err != nil || !status.Running {
					t.Fatalf("start: %#v %v", status, err)
				}
				first := ctrl.sentMessages()[0].msg.Payload.(*packets.LightSetWaveformOptional)
				if effect == DeviceEffectBreathe && math.Abs(float64(first.Color.Hue)-float64(hslColorToHSBK(*current.Color, current.Brightness, current.Kelvin, current.Capability).Hue)) > 1 {
					t.Fatal("initial frame used stale controller colour")
				}
				if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: current}); err != nil {
					t.Fatal(err)
				}
				states := ctrl.restoredStateSnapshots()
				if len(states) != 1 || states[0].Devices[0].PoweredOn != on || states[0].Devices[0].Color != lifxdevice.NewColor(hslColorToHSBK(*current.Color, current.Brightness, current.Kelvin, current.Capability)) {
					t.Fatalf("wrong restore: %#v", states)
				}
				if !on {
					sends := ctrl.sentMessages()
					if power, ok := sends[1].msg.Payload.(*packets.DeviceSetPower); !ok || power.Level == 0 {
						t.Fatal("off device was not primed before power-on")
					}
				}
			})
		}
	}
}

func TestSingleZonePreviewHonorsOptionalFields(t *testing.T) {
	state := lifxeffects.PhysicalColorState{Zones: []packets.LightHsbk{{Hue: 100, Saturation: 200, Brightness: 300, Kelvin: 4000}}}
	err := applyPreviewPacket(&state, lifxdevice.Surface{}, protocol.NewMessage(&packets.LightSetWaveformOptional{Color: packets.LightHsbk{Brightness: 500}, SetBrightness: true}))
	if err != nil {
		t.Fatal(err)
	}
	if state.Zones[0] != (packets.LightHsbk{Hue: 100, Saturation: 200, Brightness: 500, Kelvin: 4000}) {
		t.Fatal("preview overwrote unspecified colour fields")
	}
}

func TestTemporalEffectsWhiteOnlyAndSpeed(t *testing.T) {
	d := lifxdevice.Device{LightType: lifxdevice.LightTypeSingleZone}
	current := Device{Kind: DeviceKindSingle, Brightness: .6, Kelvin: 3000, Color: &HSLColor{H: 200, S: 0, L: .6, Kelvin: 3000}, Capability: DeviceCapability{HasColor: false, KelvinMin: 2500, KelvinMax: 6500}}
	for _, id := range []DeviceEffect{DeviceEffectBreathe, DeviceEffectColorCycle} {
		fast, err := newAppEffect(StartDeviceEffectRequest{Effect: id, SpeedMS: 4000}, d, current)
		if err != nil {
			t.Fatal(err)
		}
		slow, err := newAppEffect(StartDeviceEffectRequest{Effect: id, SpeedMS: 8000}, d, current)
		if err != nil {
			t.Fatal(err)
		}
		fastFrame, _ := fast.Next(time.Second)
		slowFrame, _ := slow.Next(time.Second)
		for _, frame := range []lifxeffects.Frame{fastFrame, slowFrame} {
			for _, c := range frame.Colors {
				if c.Saturation != 0 || c.Kelvin < 2500 || c.Kelvin > 6500 {
					t.Fatalf("unsupported white-only colour: %#v", c)
				}
			}
		}
		if fastFrame.Colors[0] == slowFrame.Colors[0] {
			t.Fatalf("speed did not change %s phase", id)
		}
		if _, err := newAppEffect(StartDeviceEffectRequest{Effect: id, Params: map[string]any{"palette": "invalid"}}, d, current); err == nil {
			t.Fatal("unsupported settings accepted")
		}
		if appEffectSupportedForDevice(id, DeviceKindSwitch) {
			t.Fatal("switch accepted")
		}
	}
}

func TestBreatheSwitchUsesOriginalSnapshotNotAnimatedPixels(t *testing.T) {
	transport, ctrl, current := parameterTestTransport(t)
	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner}); err != nil {
		t.Fatal(err)
	}
	transport.mu.RLock()
	baseline := transport.effects[current.Serial].snapshot
	transport.mu.RUnlock()
	// Simulate the controller observing an animation, while the original snapshot is retained.
	ctrl.mu.Lock()
	for _, colors := range ctrl.devices[0].MatrixProperties.ChainZones {
		for i := range colors {
			colors[i] = testHSBK(300)
		}
	}
	ctrl.mu.Unlock()
	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectBreathe, SpeedMS: 4000}); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectBreathe, SpeedMS: 6000}); err != nil {
		t.Fatal(err)
	}
	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: current}); err != nil {
		t.Fatal(err)
	}
	restored := ctrl.restoredStateSnapshots()
	if len(restored) != 1 || !reflect.DeepEqual(restored[0], baseline) {
		t.Fatal("switching effects or changing speed replaced the original restore state")
	}
}
