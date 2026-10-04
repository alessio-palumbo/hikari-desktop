package backend

import (
	"context"
	"errors"
	"math"
	"reflect"
	"testing"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	lifxthemes "github.com/alessio-palumbo/lifxlan-go/pkg/themes"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

func testThemeRequest(devices ...lifxdevice.Device) ThemeRequest {
	req := ThemeRequest{Theme: lifxthemes.Theme{Name: "Evening", Palette: lifxeffects.Palette{Base: []lifxeffects.Color{
		{Hue: 20, Saturation: 80, Brightness: 85, Kelvin: 3500},
		{Hue: 240, Saturation: 70, Brightness: 75, Kelvin: 4000},
	}}}}
	for _, d := range devices {
		req.Serials = append(req.Serials, d.Serial.String())
	}
	return req
}

func themeTestLight(t *testing.T, serial string) lifxdevice.Device {
	t.Helper()
	d := testLifxDevice(t, serial, "Lamp", "Home", "Desk")
	d.SetProductInfo(27)
	d.Type, d.LightType = lifxdevice.DeviceTypeLight, lifxdevice.LightTypeSingleZone
	d.Color = lifxeffects.Color{Hue: 120, Saturation: 90, Brightness: 40, Kelvin: 3500}
	return d
}

func TestThemeGroupPlanningAssignsStableDistinctColors(t *testing.T) {
	a, b := themeTestLight(t, "d073d501a2c3"), themeTestLight(t, "d073d501a2c4")
	b.Color.Brightness, b.PoweredOn = 65, false
	req := testThemeRequest(b, a)
	first, err := planThemePreview(context.Background(), req, []lifxdevice.Device{b, a})
	if err != nil {
		t.Fatal(err)
	}
	second, err := planThemePreview(context.Background(), req, []lifxdevice.Device{a, b})
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("unstable preview: %v", err)
	}
	if first.Devices[0].Serial != a.Serial.String() || first.Devices[1].Serial != b.Serial.String() || first.Devices[0].Colors[0].H == first.Devices[1].Colors[0].H {
		t.Fatalf("incorrect group palette assignment: %#v", first)
	}
	for i, brightness := range []float64{.4, .65} {
		if math.Abs(first.Devices[i].Colors[0].L-brightness) > .0001 {
			t.Fatal("single-zone brightness changed")
		}
	}
	if first.Devices[1].On {
		t.Fatal("preview powered on an off device")
	}
}

func TestThemePreservesZoneBrightnessIncludingBlack(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	d.LightType = lifxdevice.LightTypeMultiZone
	for _, brightness := range []float64{80, 40, 0, 65} {
		d.MultizoneProperties.Zones = append(d.MultizoneProperties.Zones, (lifxeffects.Color{Hue: 120, Saturation: 90, Brightness: brightness, Kelvin: 3500}).ToDeviceColor())
	}
	before := d.Clone()
	req := testThemeRequest(d)
	preview, err := planThemePreview(context.Background(), req, []lifxdevice.Device{d})
	if err != nil {
		t.Fatal(err)
	}
	for i, color := range preview.Devices[0].Colors {
		want := lifxdevice.NewColor(d.MultizoneProperties.Zones[i]).Brightness / 100
		if math.Abs(color.L-want) > .0001 {
			t.Fatalf("zone %d brightness changed: %v want %v", i, color.L, want)
		}
		if !preview.Devices[0].Cells[i] {
			t.Fatal("black emitter was hidden")
		}
	}
	req.Brightness = lifxthemes.PaletteBrightness
	palette, err := planThemePreview(context.Background(), req, []lifxdevice.Device{d})
	if err != nil || palette.Devices[0].Colors[2].L == 0 {
		t.Fatalf("explicit palette brightness not used: %v", err)
	}
	if !reflect.DeepEqual(before, d) {
		t.Fatal("planning mutated input state")
	}
}

func TestThemeWhiteOnlyUsesKelvinAndPreservesBrightness(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	d.ColorProperties = lifxdevice.ColorProperties{HasColor: false, TemperatureRange: lifxdevice.TemperatureRange{Min: 5500, Max: 6500}}
	preview, err := planThemePreview(context.Background(), testThemeRequest(d), []lifxdevice.Device{d})
	if err != nil {
		t.Fatal(err)
	}
	color := preview.Devices[0].Colors[0]
	if color.S != 0 || color.Kelvin != 5500 || math.Abs(color.L-.4) > .0001 {
		t.Fatalf("invalid white-only preview: %#v", color)
	}
}

func TestThemePreviewIrregularAndOrientedMatrices(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		product               uint32
		width, height, chains int
	}{
		{"tile chain", 55, 8, 8, 2}, {"candle", 57, 5, 11, 1},
		{"ceiling", 145, 8, 8, 1}, {"capsule", 201, 8, 16, 1}, {"luna", 219, 7, 5, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := previewTestDevice(tc.product, tc.width, tc.height, tc.chains)
			d.Serial, _ = lifxdevice.SerialFromHex("d073d501a2c3")
			d.Type = lifxdevice.DeviceTypeLight
			d.MatrixProperties.ChainOrientations = []lifxdevice.Orientation{lifxdevice.OrientationRight, lifxdevice.OrientationLeft}
			d.MatrixProperties.ChainZones = make([][]packets.LightHsbk, tc.chains)
			for chain := range d.MatrixProperties.ChainZones {
				for i := 0; i < tc.width*tc.height; i++ {
					d.MatrixProperties.ChainZones[chain] = append(d.MatrixProperties.ChainZones[chain], (lifxeffects.Color{Hue: 120, Saturation: 80, Brightness: float64((i*7 + chain*13) % 100), Kelvin: 3500}).ToDeviceColor())
				}
			}
			initial, err := lifxeffects.FrameFromDeviceState(d, 0)
			if err != nil {
				t.Fatal(err)
			}
			preview, err := planThemePreview(context.Background(), testThemeRequest(d), []lifxdevice.Device{d})
			if err != nil {
				t.Fatal(err)
			}
			for i, color := range preview.Devices[0].Colors {
				if !preview.Devices[0].Cells[i] {
					continue
				}
				if math.Abs(color.L-initial.Colors[i].Brightness/100) > .0001 {
					t.Fatalf("cell %d brightness/orientation changed", i)
				}
			}
		})
	}
}

func TestTransportThemePreviewUsesCacheWithoutChangingDevice(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := newTestLifxTransport(t, ctrl)
	current := mapLifxDevice(d, "desk")
	current.Brightness, current.Color.L, current.On = .7, .7, false
	transport.storeCachedDevice(current)
	preview, err := transport.PreviewTheme(context.Background(), testThemeRequest(d))
	if err != nil || math.Abs(preview.Devices[0].Colors[0].L-.7) > .0001 || preview.Devices[0].On {
		t.Fatalf("new cache not used: %#v %v", preview, err)
	}
	// An active Hikari animation must not become the preserved baseline.
	transport.mu.Lock()
	done := make(chan struct{})
	close(done)
	cancelled := false
	transport.effects[current.Serial] = runningAppEffect{previous: current, effect: DeviceEffectBreathe, done: done, cancel: func() { cancelled = true }}
	animated := current
	animated.Brightness = .1
	transport.cache[current.Serial] = animated
	transport.mu.Unlock()
	preview, err = transport.PreviewTheme(context.Background(), testThemeRequest(d))
	if err != nil || math.Abs(preview.Devices[0].Colors[0].L-.7) > .0001 {
		t.Fatalf("animation replaced original brightness: %v", err)
	}
	if cancelled || len(ctrl.sentMessages()) != 0 || len(ctrl.restoredStateSnapshots()) != 0 || len(transport.effects) != 1 || !reflect.DeepEqual(*transport.cachedDevice(current.Serial), animated) {
		t.Fatal("theme preview changed live state")
	}
}

func TestThemePreviewRejectsInvalidInputAndMissingObservedState(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	failure := errors.New("matrix state not observed")
	base := &fakeLifxController{devices: []lifxdevice.Device{d}}
	captures := 0
	ctrl := &snapshotBoundaryController{fakeLifxController: base, capture: func(ctx context.Context, _ []lifxdevice.Serial, _ lifxcontroller.SnapshotOptions) (lifxdevice.StateSnapshot, error) {
		captures++
		if ctx.Err() != nil {
			return lifxdevice.StateSnapshot{}, ctx.Err()
		}
		return lifxdevice.StateSnapshot{}, failure
	}}
	transport := newTestLifxTransport(t, ctrl)
	for _, modify := range []func(*ThemeRequest){
		func(r *ThemeRequest) { r.Serials = nil },
		func(r *ThemeRequest) { r.Serials = append(r.Serials, r.Serials[0]) },
		func(r *ThemeRequest) { r.Serials[0] = "invalid" },
		func(r *ThemeRequest) { r.Serials[0] = "d073d501a2c4" },
		func(r *ThemeRequest) { r.Theme.Name = "" },
		func(r *ThemeRequest) { r.Brightness = "unknown" },
	} {
		req := testThemeRequest(d)
		modify(&req)
		if _, err := transport.PreviewTheme(context.Background(), req); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if captures != 0 {
		t.Fatal("invalid request queried state")
	}
	if _, err := transport.PreviewTheme(context.Background(), testThemeRequest(d)); !errors.Is(err, failure) {
		t.Fatalf("capture failure not returned: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := transport.PreviewTheme(ctx, testThemeRequest(d)); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation not returned: %v", err)
	}
	if len(base.sentMessages()) != 0 {
		t.Fatal("failed preview sent controls")
	}
}

func TestThemePreviewBoundsAndTargetValidation(t *testing.T) {
	d := themeTestLight(t, "d073d501a2c3")
	for _, modify := range []func(*lifxdevice.Device){
		func(d *lifxdevice.Device) { d.Type = lifxdevice.DeviceTypeSwitch },
		func(d *lifxdevice.Device) { d.ProductID = 0 },
		func(d *lifxdevice.Device) {
			d.LightType = lifxdevice.LightTypeMatrix
			d.MatrixProperties.Width = math.MaxInt
		},
		func(d *lifxdevice.Device) {
			d.LightType = lifxdevice.LightTypeMultiZone
			d.MultizoneProperties.Zones = make([]packets.LightHsbk, maxEffectPreviewCells+1)
		},
	} {
		bad := d.Clone()
		modify(&bad)
		if _, err := planThemePreview(context.Background(), testThemeRequest(bad), []lifxdevice.Device{bad}); err == nil {
			t.Fatal("unsupported or oversized target accepted")
		}
	}
	req := testThemeRequest(d)
	req.Serials = make([]string, maxThemePreviewTargets+1)
	if _, err := validateThemeRequest(req); err == nil {
		t.Fatal("too many targets accepted")
	}
	if _, err := planThemePreview(context.Background(), testThemeRequest(d), nil); err == nil {
		t.Fatal("missing selected state accepted")
	}
	other := themeTestLight(t, "d073d501a2c4")
	if _, err := planThemePreview(context.Background(), testThemeRequest(d), []lifxdevice.Device{other}); err == nil {
		t.Fatal("unselected target accepted")
	}
}
