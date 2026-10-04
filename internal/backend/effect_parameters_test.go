package backend

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

func TestEffectParameterDefinitionsUseRegistryRangesAndHikariDefaults(t *testing.T) {
	for _, effect := range []DeviceEffect{DeviceEffectSnake, DeviceEffectWorm, DeviceEffectWave, DeviceEffectRing, DeviceEffectComet, DeviceEffectSparkle, DeviceEffectScanner} {
		parameters, err := EffectParameterDefinitions(effect)
		if err != nil {
			t.Fatal(err)
		}
		definition, _ := lifxeffects.Definition(lifxeffects.EffectID(effect))
		if len(parameters) != len(configurableEffectDefaults(effect)) {
			t.Fatal("missing parameters")
		}
		for _, param := range parameters {
			if param.Key == "peak_brightness_factor" && (param.Label != "peak brightness" || param.Description == "") {
				t.Fatal("missing peak brightness explanation")
			}
			if (param.Key == "background_floor" || param.Key == "background_brightness_factor") && (param.Label != "background brightness" || param.Description == "") {
				t.Fatal("missing background brightness explanation")
			}
			found := false
			for _, registered := range definition.Params {
				if registered.Key == param.Key {
					found = true
					if param.Min != *registered.Min || (registered.Max != nil && param.Max != *registered.Max) || param.Step != *registered.Step {
						t.Fatal("registry range changed")
					}
				}
			}
			if !found || param.Default != configurableEffectDefaults(effect)[param.Key] || param.Value != param.Default {
				t.Fatalf("invalid parameter %#v", param)
			}
		}
	}
	if _, err := EffectParameterDefinitions(DeviceEffectWaterfall); err == nil {
		t.Fatal("unexposed settings accepted")
	}
}

func TestConfigurableEffectsPreserveExistingPresetFrames(t *testing.T) {
	d := previewTestDevice(55, 8, 8, 1)
	current := Device{Kind: DeviceKindMatrix, Brightness: .6, Color: &HSLColor{H: 210, S: .8, L: .6}, Capability: DeviceCapability{HasColor: true}}
	for _, id := range []DeviceEffect{DeviceEffectSnake, DeviceEffectWorm, DeviceEffectWave, DeviceEffectRing, DeviceEffectComet, DeviceEffectSparkle, DeviceEffectScanner} {
		req := StartDeviceEffectRequest{Device: current, Effect: id, SpeedMS: 4000}
		var previous lifxeffects.Effect
		switch id {
		case DeviceEffectSnake:
			previous = lifxeffects.NewSnake(lifxeffects.SnakeConfig{Capabilities: appEffectCapabilities(d), Size: appEffectSnakeSize(d), Color: appEffectPrimaryColor(current)})
		case DeviceEffectWorm:
			previous = lifxeffects.NewWorm(lifxeffects.WormConfig{Capabilities: appEffectCapabilities(d), Size: appEffectSnakeSize(d), Color: appEffectPrimaryColor(current)})
		case DeviceEffectWave:
			previous = lifxeffects.NewWave(lifxeffects.WaveConfig{Capabilities: appEffectCapabilities(d), Palette: appEffectFlowPalette(current), Waves: 2})
		case DeviceEffectRing:
			previous = lifxeffects.NewRing(lifxeffects.RingConfig{Capabilities: appEffectCapabilities(d), Palette: appEffectFlowPalette(current), Period: 4 * time.Second})
		case DeviceEffectComet:
			previous = lifxeffects.NewComet(lifxeffects.CometConfig{Capabilities: appEffectCapabilities(d), Palette: appEffectCometPalette(current), Axis: lifxeffects.FlowAxisHorizontal, TailSize: 5, BackgroundBrightnessFactor: 1, PeakBrightnessFactor: 1.5, TailCurve: 3, TailSaturationFactor: .25, Period: 4 * time.Second})
		case DeviceEffectSparkle:
			previous = lifxeffects.NewSparkle(lifxeffects.SparkleConfig{Capabilities: appEffectCapabilities(d), Palette: appEffectSparklePalette(current), Density: .18, Decay: 1.2, BackgroundFloor: .55, PeakBrightnessFactor: 1.5, Seed: 1, Period: 4 * time.Second})
		case DeviceEffectScanner:
			previous = lifxeffects.NewScanner(lifxeffects.ScannerConfig{Capabilities: appEffectCapabilities(d), Palette: appEffectScannerPalette(current), Axis: lifxeffects.FlowAxisHorizontal, BackgroundBrightnessFactor: .8, PeakBrightnessFactor: 1.55, Period: 4 * time.Second})
		}
		registered, err := newAppEffect(req, d, current)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 40; i++ {
			want, wantOK := previous.Next(defaultAppEffectStep)
			got, gotOK := registered.Next(defaultAppEffectStep)
			if wantOK != gotOK || !reflect.DeepEqual(want, got) {
				t.Fatalf("%s preset differs at frame %d", id, i)
			}
		}
	}
}

func TestAdditionalEffectParametersChangePreview(t *testing.T) {
	d := previewTestDevice(55, 8, 8, 1)
	current := Device{Kind: DeviceKindMatrix, Brightness: .6, Color: &HSLColor{H: 210, S: .8, L: .6}, Capability: DeviceCapability{HasColor: true}}
	for _, tc := range []struct {
		effect DeviceEffect
		params map[string]float64
	}{
		{DeviceEffectSnake, map[string]float64{"size": 2}},
		{DeviceEffectWorm, map[string]float64{"size": 2}},
		{DeviceEffectWave, map[string]float64{"amplitude": 5, "width": 5, "waves": 3}},
		{DeviceEffectRing, map[string]float64{"width": 3, "floor": .6}},
	} {
		t.Run(string(tc.effect), func(t *testing.T) {
			req := StartDeviceEffectRequest{Device: current, Effect: tc.effect, SpeedMS: 4000}
			baseline, err := renderEffectPreview(context.Background(), req, d, current)
			if err != nil {
				t.Fatal(err)
			}
			req.Params = tc.params
			modified, err := renderEffectPreview(context.Background(), req, d, current)
			if err != nil || reflect.DeepEqual(modified, baseline) {
				t.Fatalf("preview unchanged: %v", err)
			}
		})
	}
}

func TestTrailParametersUseDeviceWidthAndRejectFractionalCounts(t *testing.T) {
	d := previewTestDevice(55, 2, 2, 1)
	parameters, err := effectParametersForDevice(DeviceEffectSnake, d)
	if err != nil {
		t.Fatal(err)
	}
	if len(parameters) != 1 || parameters[0].Max != 2 || parameters[0].Default != 2 || parameters[0].Unit != "cells" {
		t.Fatalf("invalid small matrix range: %#v", parameters)
	}
	for _, size := range []float64{1.5, 3, math.NaN(), math.Inf(1)} {
		if _, err := newAppEffect(StartDeviceEffectRequest{Effect: DeviceEffectSnake, Params: map[string]float64{"size": size}}, d, Device{Kind: DeviceKindMatrix}); err == nil {
			t.Fatalf("invalid size accepted: %v", size)
		}
	}
	base := StartDeviceEffectRequest{Effect: DeviceEffectSnake, SpeedMS: 1000}
	short := base
	short.Params = map[string]float64{"size": 1}
	if appEffectStep(short, d) <= appEffectStep(base, d) {
		t.Fatal("speed calculation ignored trail length")
	}
}

func TestCometParametersChangeStripPreview(t *testing.T) {
	d := lifxdevice.Device{LightType: lifxdevice.LightTypeMultiZone, MultizoneProperties: lifxdevice.MultizoneProperties{Zones: make([]packets.LightHsbk, 32)}}
	current := Device{Kind: DeviceKindMultizone, Brightness: .6, Color: &HSLColor{H: 210, S: .8, L: .6}, Capability: DeviceCapability{HasColor: true}}
	req := StartDeviceEffectRequest{Device: current, Effect: DeviceEffectComet, SpeedMS: 4000}
	baseline, err := renderEffectPreview(context.Background(), req, d, current)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]float64{"tail_size": 12, "background_brightness_factor": .4, "peak_brightness_factor": 1, "tail_curve": .5, "tail_saturation_factor": 1} {
		req.Params = map[string]float64{key: value}
		modified, err := renderEffectPreview(context.Background(), req, d, current)
		if err != nil || reflect.DeepEqual(modified, baseline) {
			t.Fatalf("comet %s preview unchanged: %v", key, err)
		}
	}
}

func TestEffectParametersValidateAndChangePreview(t *testing.T) {
	d := previewTestDevice(55, 8, 8, 1)
	current := Device{Kind: DeviceKindMatrix, Brightness: .6, Color: &HSLColor{H: 210, S: .8, L: .6}, Capability: DeviceCapability{HasColor: true}}
	req := StartDeviceEffectRequest{Device: current, Effect: DeviceEffectSparkle, SpeedMS: 4000}
	baseline, err := renderEffectPreview(context.Background(), req, d, current)
	if err != nil {
		t.Fatal(err)
	}
	req.Params = map[string]float64{"density": .7, "background_floor": .2}
	modified, err := renderEffectPreview(context.Background(), req, d, current)
	if err != nil || reflect.DeepEqual(modified, baseline) {
		t.Fatalf("preview unchanged: %v", err)
	}
	for _, params := range []map[string]float64{{"density": 2}, {"density": math.NaN()}, {"density": math.Inf(1)}, {"unknown": 1}, {"seed": 5}, {"period": 1}} {
		req.Params = params
		if _, err := newAppEffect(req, d, current); err == nil {
			t.Fatalf("invalid settings accepted: %v", params)
		}
	}
}

func parameterTestTransport(t *testing.T) (*LifxTransport, *fakeLifxController, Device) {
	t.Helper()
	d := testLifxDevice(t, "d073d501a2c3", "Tiles", "Home", "Desk")
	d.SetProductInfo(55)
	d.MatrixProperties = lifxdevice.MatrixProperties{Width: 2, Height: 2, NZones: 4, ChainLength: 1, ChainZones: [][]packets.LightHsbk{{testHSBK(20), testHSBK(60), testHSBK(120), testHSBK(240)}}}
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}, sent: make(chan sentMessage, 32)}
	transport := newTestLifxTransport(t, ctrl)
	current := mapLifxDevice(d, "desk")
	transport.storeCachedDevice(current)
	return transport, ctrl, current
}

func TestInvalidSettingsLeaveRunningEffectUntouched(t *testing.T) {
	transport, ctrl, current := parameterTestTransport(t)
	canceled := false
	done := make(chan struct{})
	close(done)
	transport.effects[current.Serial] = runningAppEffect{effect: DeviceEffectScanner, previous: current, cancel: func() { canceled = true }, done: done}
	_, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner, Params: map[string]float64{"peak_brightness_factor": 100}})
	if err == nil || canceled || len(ctrl.sentMessages()) != 0 {
		t.Fatalf("invalid settings affected runner: %v", err)
	}
	transport.mu.RLock()
	remaining := len(transport.effects)
	transport.mu.RUnlock()
	if remaining != 1 {
		t.Fatal("active effect was removed")
	}
}

func TestAppliedEffectParametersSurviveRestartsAndRestoreOriginalState(t *testing.T) {
	transport, ctrl, current := parameterTestTransport(t)
	params := map[string]float64{"background_brightness_factor": .5, "peak_brightness_factor": 1.8}
	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner, SpeedMS: 4000, Params: params}); err != nil {
		t.Fatal(err)
	}
	waitForTileSet64(t, ctrl.sent)
	params["background_brightness_factor"] = .1
	if _, err := transport.StartDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectScanner, SpeedMS: 6000}); err != nil {
		t.Fatal(err)
	}
	parameters, err := transport.EffectParameters(current.Serial, DeviceEffectScanner)
	if err != nil {
		t.Fatal(err)
	}
	for _, param := range parameters {
		if param.Key == "background_brightness_factor" && param.Value != .5 {
			t.Fatal("parameters aliased or lost on speed change")
		}
	}
	changed := current
	changed.Brightness = .3
	for i := range changed.Chain {
		changed.Chain[i].Pixels = append([]HSLColor(nil), current.Chain[i].Pixels...)
		for j := range changed.Chain[i].Pixels {
			changed.Chain[i].Pixels[j].L = .3
		}
	}
	if _, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: changed, Intent: DeviceCommandBrightness}); err != nil {
		t.Fatal(err)
	}
	transport.mu.RLock()
	active := transport.effects[current.Serial]
	transport.mu.RUnlock()
	if active.params["background_brightness_factor"] != .5 || active.params["peak_brightness_factor"] != 1.8 {
		t.Fatal("parameters lost on state restart")
	}
	if _, err := transport.StopDeviceEffect(context.Background(), StopDeviceEffectRequest{Device: changed}); err != nil {
		t.Fatal(err)
	}
	restored := restoredDeviceState(t, ctrl)
	if lifxdevice.NewColor(restored.MatrixChains[0][0]).Brightness != 30 {
		t.Fatal("restore did not retain the newer user brightness")
	}
}
