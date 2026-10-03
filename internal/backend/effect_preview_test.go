package backend

import (
	"context"
	"reflect"
	"testing"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	lifxadapters "github.com/alessio-palumbo/lifxlan-go/pkg/effects/adapters"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

func previewTestDevice(productID uint32, width, height, chainLength int) lifxdevice.Device {
	d := lifxdevice.Device{MatrixProperties: lifxdevice.MatrixProperties{Width: width, Height: height, NZones: width * height, ChainLength: chainLength}}
	d.SetProductInfo(productID)
	// A completed matrix-state handshake supplies the actual light type.
	d.LightType = lifxdevice.LightTypeMatrix
	return d
}

func TestEffectPreviewNativeRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		product               uint32
		width, height, chains int
	}{
		{"tile chain", 55, 8, 8, 2},
		{"candle", 57, 5, 11, 1},
		{"ceiling", 145, 8, 8, 1},
		{"capsule", 201, 8, 16, 1},
		{"luna", 219, 7, 5, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := previewTestDevice(tc.product, tc.width, tc.height, tc.chains)
			d.MatrixProperties.ChainOrientations = []lifxdevice.Orientation{lifxdevice.OrientationRight, lifxdevice.OrientationLeft}
			surface := lifxdevice.SurfaceFromDevice(d)
			frame := lifxeffects.NewFrame(surface.Width, surface.Height, time.Millisecond, lifxeffects.Color{})
			for i := range frame.Colors {
				frame.Colors[i] = lifxeffects.Color{Hue: float64(i * 17 % 360), Saturation: 80, Brightness: 60, Kelvin: 3500}
			}
			state := lifxeffects.NewPhysicalColorState(surface)
			packetsSent := 0
			renderer := lifxadapters.NewRendererForDevice(d, func(msg *protocol.Message) error { packetsSent++; return applyPreviewPacket(&state, surface, msg) })
			if err := renderer.RenderFrame(context.Background(), frame); err != nil {
				t.Fatal(err)
			}
			got, err := lifxeffects.AdaptPhysicalColorStateToFrame(state, surface, frame.Duration)
			if err != nil {
				t.Fatal(err)
			}
			if packetsSent < tc.chains {
				t.Fatalf("packet count %d", packetsSent)
			}
			for i, color := range got.Colors {
				if color.Brightness == 0 {
					continue
				} // Non-emitting/hidden cells.
				if color.ToDeviceColor() != frame.Colors[i].ToDeviceColor() {
					t.Fatalf("cell %d: got %#v, want %#v", i, color, frame.Colors[i])
				}
			}
		})
	}
}

func TestEffectPreviewMultizoneChunking(t *testing.T) {
	d := lifxdevice.Device{LightType: lifxdevice.LightTypeMultiZone, MultizoneProperties: lifxdevice.MultizoneProperties{Zones: make([]packets.LightHsbk, 120)}}
	surface := lifxdevice.SurfaceFromDevice(d)
	state := lifxeffects.NewPhysicalColorState(surface)
	frame := lifxeffects.NewFrame(120, 1, time.Millisecond, lifxeffects.Color{Hue: 120, Saturation: 80, Brightness: 50, Kelvin: 3500})
	frame.Colors[119].Hue = 240
	renderer := lifxadapters.NewRendererForDevice(d, func(msg *protocol.Message) error { return applyPreviewPacket(&state, surface, msg) })
	if err := renderer.RenderFrame(context.Background(), frame); err != nil {
		t.Fatal(err)
	}
	got, err := lifxeffects.AdaptPhysicalColorStateToFrame(state, surface, frame.Duration)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got.Colors {
		if got.Colors[i].ToDeviceColor() != frame.Colors[i].ToDeviceColor() {
			t.Fatalf("cell %d not preserved", i)
		}
	}
}

func TestEffectPreviewIsDeterministicBoundedAndCancellable(t *testing.T) {
	d := previewTestDevice(55, 8, 8, 2)
	current := Device{Kind: DeviceKindMatrix, Brightness: .6, Color: &HSLColor{H: 210, S: .8, L: .6}, Capability: DeviceCapability{HasColor: true}}
	req := StartDeviceEffectRequest{Device: current, Effect: DeviceEffectSnake, SpeedMS: 12000}
	first, err := renderEffectPreview(context.Background(), req, d, current)
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderEffectPreview(context.Background(), req, d, current)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("nondeterministic preview: %v", err)
	}
	if first.Width != 16 || first.Height != 8 || len(first.Frames)*first.Width*first.Height > maxEffectPreviewColorValues || len(first.Frames) > maxEffectPreviewFrames {
		t.Fatalf("invalid preview size %#v", first)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := renderEffectPreview(ctx, req, d, current); err == nil {
		t.Fatal("canceled preview accepted")
	}
	req.Effect = DeviceEffectMorph
	if _, err := renderEffectPreview(context.Background(), req, d, current); err == nil {
		t.Fatal("firmware preview accepted")
	}
	req.Effect = DeviceEffectSnake
	d.MatrixProperties.Width = 8192
	if _, err := renderEffectPreview(context.Background(), req, d, current); err == nil {
		t.Fatal("oversized preview accepted")
	}
}

func TestTransportEffectPreviewDoesNotSendOrMutateState(t *testing.T) {
	d := previewTestDevice(55, 8, 8, 1)
	d.Serial, _ = lifxdevice.SerialFromHex("d073d501a2c3")
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := NewLifxTransportWithController(ctrl)
	current := Device{Serial: "d073d501a2c3", Kind: DeviceKindMatrix, On: false, Brightness: .6, Color: &HSLColor{H: 210, S: .8, L: .6}, Capability: DeviceCapability{HasColor: true}}
	transport.storeCachedDevice(current)
	transport.effects[current.Serial] = runningAppEffect{previous: current, effect: DeviceEffectSnake}
	got, err := transport.PreviewDeviceEffect(context.Background(), StartDeviceEffectRequest{Device: current, Effect: DeviceEffectSnake, SpeedMS: 12000})
	if err != nil || len(got.Frames) == 0 {
		t.Fatalf("preview: %v", err)
	}
	if len(ctrl.sends) != 0 || len(ctrl.restores) != 0 || len(transport.effects) != 1 || !reflect.DeepEqual(*transport.cachedDevice(current.Serial), current) {
		t.Fatal("preview changed live state")
	}
}

func TestAllHikariEffectsCanPreview(t *testing.T) {
	for _, kind := range []DeviceKind{DeviceKindMatrix, DeviceKindMultizone} {
		for _, effect := range []DeviceEffect{DeviceEffectSnake, DeviceEffectWorm, DeviceEffectFrames, DeviceEffectWaterfall, DeviceEffectRockets, DeviceEffectWave, DeviceEffectRing, DeviceEffectFlow, DeviceEffectComet, DeviceEffectSparkle, DeviceEffectScanner} {
			if !appEffectSupportedForDevice(effect, kind) {
				continue
			}
			t.Run(string(kind)+"/"+string(effect), func(t *testing.T) {
				d := previewTestDevice(55, 8, 8, 1)
				if kind == DeviceKindMultizone {
					d = lifxdevice.Device{LightType: lifxdevice.LightTypeMultiZone, MultizoneProperties: lifxdevice.MultizoneProperties{Zones: make([]packets.LightHsbk, 32)}}
				}
				current := Device{Kind: kind, Brightness: .6, Color: &HSLColor{H: 210, S: .8, L: .6}, Capability: DeviceCapability{HasColor: true}}
				preview, err := renderEffectPreview(context.Background(), StartDeviceEffectRequest{Device: current, Effect: effect, SpeedMS: 4000}, d, current)
				if err != nil {
					t.Fatal(err)
				}
				if len(preview.Frames) < 2 || len(preview.Cells) != preview.Width*preview.Height {
					t.Fatal("missing preview frames or mask")
				}
				lit := false
				for _, frame := range preview.Frames {
					if len(frame) != len(preview.Cells) {
						t.Fatal("invalid frame size")
					}
					for i, color := range frame {
						if preview.Cells[i] && color.L > 0 {
							lit = true
						}
					}
				}
				if !lit {
					t.Fatal("preview is entirely blank")
				}
			})
		}
	}
}
