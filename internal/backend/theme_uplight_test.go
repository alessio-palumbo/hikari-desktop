package backend

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	lifxthemes "github.com/alessio-palumbo/lifxlan-go/pkg/themes"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

func themeUplightDevice(t *testing.T, product uint32, width, height int, orientation lifxdevice.Orientation, upBrightness uint16) lifxdevice.Device {
	t.Helper()
	d := previewTestDevice(product, width, height, 1)
	d.Serial, _ = lifxdevice.SerialFromHex("d073d501a2c3")
	d.Type, d.PoweredOn = lifxdevice.DeviceTypeLight, true
	d.MatrixProperties.ChainOrientations = []lifxdevice.Orientation{orientation}
	d.MatrixProperties.ChainZones = [][]packets.LightHsbk{make([]packets.LightHsbk, width*height)}
	for i := range width * height {
		d.MatrixProperties.ChainZones[0][i] = packets.LightHsbk{Hue: uint16(1200 + i*37), Saturation: 32000, Brightness: uint16(i * 401), Kelvin: 3500}
	}
	mask, err := lifxeffects.LightPartPhysicalMask(d, lifxdevice.LightPartUplight)
	if err != nil {
		t.Fatal(err)
	}
	for i, cell := range mask {
		if cell.Brightness > 0 {
			d.MatrixProperties.ChainZones[0][i].Brightness = upBrightness
		}
	}
	return d
}

func TestThemeUplightPreviewAndApplyUseExactComposedPlan(t *testing.T) {
	for _, geometry := range []struct {
		product       uint32
		width, height int
	}{{265, 8, 8}, {201, 8, 16}, {201, 16, 8}} {
		for _, orientation := range []lifxdevice.Orientation{lifxdevice.OrientationRightSideUp, lifxdevice.OrientationLeft, lifxdevice.OrientationRight} {
			t.Run(fmt.Sprintf("%d/%dx%d/%d", geometry.product, geometry.width, geometry.height, orientation), func(t *testing.T) {
				d := themeUplightDevice(t, geometry.product, geometry.width, geometry.height, orientation, 60000)
				before := d.Clone()
				ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
				transport := newTestLifxTransport(t, ctrl)
				req := testThemeRequest(d)
				req.Variation, req.MatrixLayout = 3, lifxthemes.MatrixSpatial
				native, err := req.Theme.PlanParts([]lifxdevice.Device{d}, defaultColorTransitionDuration, lifxthemes.PartPlanOptions{Part: lifxdevice.LightPartAll, Brightness: lifxthemes.PreserveBrightness, Variation: 3, MatrixLayout: lifxthemes.MatrixSpatial})
				if err != nil {
					t.Fatal(err)
				}
				preview, err := transport.PreviewTheme(context.Background(), req)
				if err != nil || len(preview.Devices) != 1 || preview.Devices[0].Uplight == nil || len(ctrl.sentMessages()) != 0 {
					t.Fatalf("uplight preview=%#v error=%v", preview, err)
				}
				result, err := transport.ApplyTheme(context.Background(), req)
				if err != nil || len(result.Failures) != 0 || len(result.Devices) != 1 {
					t.Fatalf("apply=%#v error=%v", result, err)
				}
				sends := ctrl.sentMessages()
				if len(sends) != len(native[0].Messages) {
					t.Fatal("native composed packets were replaced or redundant power was sent")
				}
				surface := lifxdevice.SurfaceFromDevice(d)
				physical := lifxeffects.NewPhysicalColorState(surface)
				for i, send := range sends {
					if send.serial != d.Serial || !reflect.DeepEqual(send.msg.Payload, native[0].Messages[i].Payload) {
						t.Fatal("uplight application diverged from library packets")
					}
					if err := applyPreviewPacket(&physical, surface, send.msg); err != nil {
						t.Fatal(err)
					}
				}
				upMask, _ := lifxeffects.LightPartPhysicalMask(d, lifxdevice.LightPartUplight)
				mainMask, _ := lifxeffects.LightPartPhysicalMask(d, lifxdevice.LightPartMain)
				for i, color := range physical.MatrixChains[0] {
					if color.Brightness != before.MatrixProperties.ChainZones[0][i].Brightness {
						t.Fatalf("physical brightness changed at %d", i)
					}
					if upMask[i].Brightness > 0 {
						want := mapLifxHSBK(color, result.Devices[0].Capability)
						if !reflect.DeepEqual(*preview.Devices[0].Uplight, want) || color.Hue == before.MatrixProperties.ChainZones[0][i].Hue {
							t.Fatal("uplight missing from theme or preview differs from sent color")
						}
					} else if mainMask[i].Brightness == 0 && color != before.MatrixProperties.ChainZones[0][i] {
						t.Fatal("non-emitter hidden cells changed")
					}
				}
				logical, err := lifxeffects.AdaptPhysicalColorStateToFrame(physical, surface, 0)
				if err != nil {
					t.Fatal(err)
				}
				for i, color := range logical.Colors {
					if !preview.Devices[0].Cells[i] {
						continue
					}
					want := mapLifxColor(color, result.Devices[0].Capability)
					if !reflect.DeepEqual(preview.Devices[0].Colors[i], want) {
						t.Fatalf("main preview/send mismatch at %d", i)
					}
				}
				if math.Abs(result.Devices[0].Brightness-native[0].Uplight.Brightness/100) > .0001 || !reflect.DeepEqual(d, before) {
					t.Fatal("aggregate brightness changed or input was mutated")
				}
			})
		}
	}
}

func TestThemePreservesBlackUplightAndLeavesOrdinaryPreviewUnchanged(t *testing.T) {
	up := themeUplightDevice(t, 265, 8, 8, lifxdevice.OrientationRightSideUp, 0)
	ordinary := themeTestLight(t, "d073d501a2c4")
	preview, err := planThemePreview(context.Background(), testThemeRequest(up, ordinary), []lifxdevice.Device{up, ordinary})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Devices) != 2 || preview.Devices[0].Uplight == nil || preview.Devices[0].Uplight.L != 0 || preview.Devices[1].Uplight != nil {
		t.Fatal("black uplight was brightened or ordinary light acquired an uplight")
	}
	changed := testThemeRequest(up)
	changed.Brightness = lifxthemes.PaletteBrightness
	palette, err := planThemePreview(context.Background(), changed, []lifxdevice.Device{up})
	if err != nil || palette.Devices[0].Uplight.L == 0 {
		t.Fatalf("palette brightness not forwarded: %v", err)
	}
}

func TestThemeGuardKeepsLivePartObservations(t *testing.T) {
	d := themeUplightDevice(t, 265, 8, 8, lifxdevice.OrientationRightSideUp, 0)
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := newTestLifxTransport(t, ctrl)
	result, err := transport.ApplyTheme(context.Background(), testThemeRequest(d))
	if err != nil {
		t.Fatal(err)
	}
	live := mapLifxDevice(d, result.Devices[0].GroupID)
	live.LightPartStates = map[lifxdevice.LightPart]LightPartState{
		lifxdevice.LightPartAll:     {Known: true, On: true, DevicePoweredOn: true, Brightness: live.Brightness},
		lifxdevice.LightPartUplight: {Known: true, On: false, DevicePoweredOn: true, Brightness: 0},
	}
	snapshot := DeviceSnapshot{Devices: []Device{live}}
	transport.reconcileRestoreSnapshot(&snapshot, time.Now())
	transport.replaceCache(snapshot.Devices)
	if !reflect.DeepEqual(snapshot.Devices[0].LightPartStates, live.LightPartStates) || !reflect.DeepEqual(transport.cachedDevice(d.Serial.String()).LightPartStates, live.LightPartStates) {
		t.Fatal("theme guard masked authoritative part power observations")
	}
}

func TestUplightThemePreparesColorsBeforePowerAndKeepsBlackPartsBlack(t *testing.T) {
	for _, poweredOn := range []bool{false, true} {
		t.Run(fmt.Sprint(poweredOn), func(t *testing.T) {
			d := themeUplightDevice(t, 265, 8, 8, lifxdevice.OrientationRightSideUp, 0)
			d.PoweredOn = poweredOn
			for i := range d.MatrixProperties.ChainZones[0] {
				d.MatrixProperties.ChainZones[0][i].Brightness = 0
			}
			ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
			transport := newTestLifxTransport(t, ctrl)
			before := mapLifxDevice(d, "desk")
			before.On = false
			before.LightPartStates = map[lifxdevice.LightPart]LightPartState{
				lifxdevice.LightPartAll: {Known: true, On: false, DevicePoweredOn: poweredOn},
			}
			transport.storeCachedDevice(before)
			result, err := transport.ApplyTheme(context.Background(), testThemeRequest(d))
			if err != nil || len(result.Failures) != 0 || len(result.Devices) != 1 {
				t.Fatalf("apply=%#v error=%v", result, err)
			}
			if result.Devices[0].Brightness != 0 || result.Devices[0].On {
				t.Fatal("theme invented brightness or effective part power")
			}
			sends := ctrl.sentMessages()
			if _, ok := sends[0].msg.Payload.(*packets.TileSet64); !ok {
				t.Fatal("colour was not prepared first")
			}
			if poweredOn {
				if len(sends) != 1 {
					t.Fatal("effective off was mistaken for shared-device off")
				}
			} else if len(sends) != 2 {
				t.Fatal("off device was not powered on after colour preparation")
			} else if power, ok := sends[1].msg.Payload.(*packets.DeviceSetPower); !ok || power.Level != 65535 {
				t.Fatal("power-on order changed")
			}
		})
	}
}

func TestUplightThemeRejectsIncompleteStateBeforeControlWrites(t *testing.T) {
	d := themeUplightDevice(t, 265, 8, 8, lifxdevice.OrientationRightSideUp, 40000)
	d.MatrixProperties.ChainZones[0] = d.MatrixProperties.ChainZones[0][:63]
	ctrl := &fakeLifxController{devices: []lifxdevice.Device{d}}
	transport := newTestLifxTransport(t, ctrl)
	if _, err := transport.ApplyTheme(context.Background(), testThemeRequest(d)); err == nil {
		t.Fatal("incomplete part state accepted")
	}
	if len(ctrl.sentMessages()) != 0 {
		t.Fatal("failed preflight sent controls")
	}
}
