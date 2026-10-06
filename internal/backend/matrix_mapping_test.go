package backend

import (
	"fmt"
	"reflect"
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

var matrixOrientations = []lifxdevice.Orientation{
	lifxdevice.OrientationRightSideUp, lifxdevice.OrientationUpsideDown,
	lifxdevice.OrientationFaceUp, lifxdevice.OrientationFaceDown,
	lifxdevice.OrientationLeft, lifxdevice.OrientationRight,
}

func TestMatrixInfoCountsVisiblePixelsWithoutChangingPhysicalBuffers(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		product                        uint32
		width, height, chains, visible int
	}{
		{"small Candle", 215, 5, 6, 1, 27},
		{"Candle", 57, 5, 11, 1, 52},
		{"Ceiling", 145, 8, 8, 1, 56},
		{"Capsule", 201, 8, 16, 1, 120},
		{"Luna", 219, 7, 5, 1, 31},
		{"Tile chain", 55, 8, 8, 2, 128},
		{"unknown rectangular", 999999, 3, 2, 1, 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			physicalCount := tc.width * tc.height
			d := lifxdevice.Device{
				Type: lifxdevice.DeviceTypeLight, LightType: lifxdevice.LightTypeMatrix, ProductID: tc.product,
				MatrixProperties: lifxdevice.MatrixProperties{
					Width: tc.width, Height: tc.height, NZones: physicalCount, ChainLength: tc.chains,
					ChainZones: make([][]packets.LightHsbk, tc.chains),
				},
			}
			for chain := range tc.chains {
				d.MatrixProperties.ChainZones[chain] = make([]packets.LightHsbk, physicalCount)
			}
			before := d.Clone()
			mapped := mapLifxDevice(d, "group")
			if mapped.PixelCount != tc.visible || mapped.ChainLen != tc.chains {
				t.Fatalf("pixels=%d chains=%d, want %d/%d", mapped.PixelCount, mapped.ChainLen, tc.visible, tc.chains)
			}
			if !reflect.DeepEqual(d, before) {
				t.Fatal("mapping mutated physical state")
			}
			if len(mapped.Chain) != tc.chains {
				t.Fatal("mapping dropped chain entries")
			}
			for _, matrix := range mapped.Chain {
				if len(matrix.Pixels) != physicalCount || matrix.SendWidth != tc.width {
					t.Fatal("visible count changed physical buffer dimensions")
				}
			}
		})
	}
}

func TestMatrixInfoRetainsReportedCountWhenGeometryIsUnknown(t *testing.T) {
	d := lifxdevice.Device{
		Type: lifxdevice.DeviceTypeLight, LightType: lifxdevice.LightTypeMatrix,
		MatrixProperties: lifxdevice.MatrixProperties{NZones: 30},
	}
	if mapped := mapLifxDevice(d, "group"); mapped.PixelCount != 30 {
		t.Fatalf("pixels=%d, want reported count 30", mapped.PixelCount)
	}
}

func TestNativeMatrixMappingPreservesSquarePreviewAndSendOrder(t *testing.T) {
	colors := make([]packets.LightHsbk, 64)
	for i := range colors {
		colors[i] = packets.LightHsbk{Hue: uint16(i)}
	}
	for _, orientation := range matrixOrientations {
		t.Run(fmt.Sprint(orientation), func(t *testing.T) {
			// Freeze the previously working square-grid conversion as a parity
			// oracle; production code no longer owns this switch.
			legacy := colors
			switch orientation {
			case lifxdevice.OrientationRight:
				legacy = lifxdevice.RotateMatrix(lifxdevice.RotateMatrix90(8, 8), colors)
			case lifxdevice.OrientationLeft:
				legacy = lifxdevice.RotateMatrix(lifxdevice.RotateMatrix270(8, 8), colors)
			case lifxdevice.OrientationUpsideDown:
				legacy = lifxdevice.RotateMatrix(lifxdevice.RotateMatrix180(8, 8), colors)
			}
			logical := lifxdevice.PhysicalMatrixColorsToLogical(8, 8, orientation, colors)
			if !reflect.DeepEqual(logical, legacy) {
				t.Fatal("square preview changed")
			}
			matrix := Matrix{SendWidth: 8, Orientation: int(orientation), Pixels: make([]HSLColor, 64)}
			got := rotateMatrixForOrientation(matrix, logical)
			if !reflect.DeepEqual(got, lifxdevice.ReorientMatrix(8, 8, orientation, logical)) || !reflect.DeepEqual(got, colors) {
				t.Fatal("square send order changed")
			}
		})
	}
}

func TestMatrixSnapshotToEditorToPacketsPreservesPhysicalOrder(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		product               uint32
		width, height, chains int
	}{
		{"Tile chain", 55, 8, 8, 2}, {"Candle", 57, 5, 11, 1},
		{"Ceiling", 265, 8, 8, 1}, {"Capsule", 201, 8, 16, 1},
		{"Luna", 219, 7, 5, 1}, {"unknown rectangular", 999999, 3, 2, 1},
	} {
		for _, orientation := range matrixOrientations {
			t.Run(fmt.Sprintf("%s/%d", tc.name, orientation), func(t *testing.T) {
				d := lifxdevice.Device{ProductID: tc.product, LightType: lifxdevice.LightTypeMatrix, MatrixProperties: lifxdevice.MatrixProperties{Width: tc.width, Height: tc.height, NZones: tc.width * tc.height, ChainZones: make([][]packets.LightHsbk, tc.chains), ChainOrientations: make([]lifxdevice.Orientation, tc.chains)}}
				for chain := range tc.chains {
					d.MatrixProperties.ChainOrientations[chain] = orientation
					if chain > 0 {
						d.MatrixProperties.ChainOrientations[chain] = lifxdevice.OrientationLeft
					}
					colors := make([]packets.LightHsbk, tc.width*tc.height)
					for i := range colors {
						colors[i] = packets.LightHsbk{Hue: uint16(i*317 + chain*1000), Saturation: 32000, Brightness: uint16(29000 + i%10*100), Kelvin: uint16(2500 + i%10*100)}
					}
					d.MatrixProperties.ChainZones[chain] = colors
				}
				capability := DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000}
				matrix := mapLifxMatrixChain(d, capability)
				if len(matrix) != tc.chains {
					t.Fatal("chain entries lost")
				}
				physical := lifxeffects.NewPhysicalColorState(lifxdevice.SurfaceFromDevice(d))
				for _, msg := range deviceStateMessages(Device{Kind: DeviceKindMatrix, Chain: matrix, Capability: capability}, false) {
					if p, ok := msg.Payload.(*packets.TileSet64); ok && p.Length != 1 {
						t.Fatalf("chain length %d, want 1", p.Length)
					}
					if err := applyPreviewPacket(&physical, lifxdevice.SurfaceFromDevice(d), msg); err != nil {
						t.Fatal(err)
					}
				}
				for chain, expected := range d.MatrixProperties.ChainZones {
					for i, color := range expected {
						// Keep the existing HSL/HSBK conversion (including percent
						// rounding and chromatic Kelvin fallback) out of this audit.
						color = hslColorToHSBK(mapLifxHSBK(color, capability), 1, 3500, capability)
						got := physical.MatrixChains[chain][i]
						if absInt(int(got.Hue)-int(color.Hue)) > 1 || absInt(int(got.Saturation)-int(color.Saturation)) > 1 || absInt(int(got.Brightness)-int(color.Brightness)) > 1 || got.Kelvin != color.Kelvin {
							t.Fatalf("chain %d cell %d: got %#v, want %#v", chain, i, got, color)
						}
					}
				}
				if matrix[0].SendWidth != tc.width {
					t.Fatal("packet width changed")
				}
			})
		}
	}
}

func TestMatrixMappingPadsPartialRowWithoutDroppingPixels(t *testing.T) {
	for _, orientation := range matrixOrientations {
		d := lifxdevice.Device{LightType: lifxdevice.LightTypeMatrix, MatrixProperties: lifxdevice.MatrixProperties{Width: 3, Height: 2, NZones: 6, ChainOrientations: []lifxdevice.Orientation{orientation}, ChainZones: [][]packets.LightHsbk{{testHSBK(10), testHSBK(20), testHSBK(30), testHSBK(40), testHSBK(50)}}}}
		capability := DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000}
		matrix := mapLifxMatrixChain(d, capability)[0]
		if len(matrix.Pixels) != 6 || matrixHeight(matrix) != 2 {
			t.Fatal("partial row lost or not padded")
		}
		got := rotateMatrixForOrientation(matrix, hslColorsToHSBK(matrix.Pixels, 1, 3500, capability))
		for i, color := range d.MatrixProperties.ChainZones[0] {
			if absInt(int(got[i].Hue)-int(color.Hue)) > 1 || absInt(int(got[i].Brightness)-int(color.Brightness)) > 1 {
				t.Fatalf("orientation %d cell %d changed", orientation, i)
			}
		}
		if got[5].Brightness != 0 {
			t.Fatal("padded pixel is not dark")
		}
	}
	matrix := Matrix{SendWidth: 3, Pixels: make([]HSLColor, 5)}
	if matrixHeight(matrix) != 2 {
		t.Fatal("editor partial row was rounded down")
	}
}

func TestRectangularMatrixPreviewUsesTrueInverse(t *testing.T) {
	physical := []packets.LightHsbk{{Hue: 1}, {Hue: 2}, {Hue: 3}, {Hue: 4}, {Hue: 5}, {Hue: 6}}
	logical := lifxdevice.PhysicalMatrixColorsToLogical(3, 2, lifxdevice.OrientationRight, physical)
	expected := []packets.LightHsbk{{Hue: 5}, {Hue: 3}, {Hue: 1}, {Hue: 6}, {Hue: 4}, {Hue: 2}}
	if !reflect.DeepEqual(logical, expected) {
		t.Fatalf("rectangular logical order = %#v", logical)
	}
	matrix := Matrix{SendWidth: 3, Orientation: int(lifxdevice.OrientationRight), Pixels: make([]HSLColor, 6)}
	if !reflect.DeepEqual(rotateMatrixForOrientation(matrix, logical), physical) {
		t.Fatal("rectangular round trip reordered physical pixels")
	}
	legacy := lifxdevice.RotateMatrix(lifxdevice.RotateMatrix90(3, 2), physical)
	if reflect.DeepEqual(rotateMatrixForOrientation(matrix, legacy), physical) {
		t.Fatal("fixture no longer demonstrates the old rectangular mismatch")
	}
}
