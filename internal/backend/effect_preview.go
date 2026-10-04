package backend

import (
	"context"
	"fmt"
	"maps"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	lifxadapters "github.com/alessio-palumbo/lifxlan-go/pkg/effects/adapters"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	"github.com/alessio-palumbo/lifxprotocol-go/gen/protocol/packets"
)

const maxEffectPreviewCells = 4096
const maxEffectPreviewColorValues = 40000
const maxEffectPreviewFrames = 120

type DeviceEffectPreviewer interface {
	PreviewDeviceEffect(context.Context, StartDeviceEffectRequest) (DeviceEffectPreview, error)
}

type DeviceEffectPreview struct {
	Width  int          `json:"width"`
	Height int          `json:"height"`
	StepMS int64        `json:"stepMs"`
	Cells  []bool       `json:"cells"`
	Frames [][]HSLColor `json:"frames"`
}

func (t *LifxTransport) PreviewDeviceEffect(ctx context.Context, req StartDeviceEffectRequest) (DeviceEffectPreview, error) {
	serial, err := parseDeviceSerial(req.Device)
	if err != nil {
		return DeviceEffectPreview{}, err
	}
	ctrl, err := t.requireController()
	if err != nil {
		return DeviceEffectPreview{}, err
	}
	d, ok := ctrl.GetDevice(serial)
	if !ok {
		return DeviceEffectPreview{}, fmt.Errorf("device is no longer available")
	}
	current := t.effectRequestDevice(mapLifxDevice(d, req.Device.GroupID))
	t.mu.RLock()
	if active, ok := t.effects[req.Device.Serial]; ok {
		current = active.previous
		if req.Params == nil && active.effect == req.Effect {
			req.Params = maps.Clone(active.params)
		}
	}
	t.mu.RUnlock()
	req.Device = current
	return renderEffectPreview(ctx, req, d, current)
}

// This deliberately uses the real effect factory and packet renderer, but its
// send callback writes ONLY to local physical storage. No controller sends,
// runner, restore state, power changes, or cached state mutations are involved.
func renderEffectPreview(ctx context.Context, req StartDeviceEffectRequest, d lifxdevice.Device, current Device) (DeviceEffectPreview, error) {
	if !appEffectSupportedForDevice(req.Effect, current.Kind) {
		return DeviceEffectPreview{}, fmt.Errorf("local preview is unavailable for this effect")
	}
	if req.SpeedMS < 0 || req.SpeedMS > 30000 {
		return DeviceEffectPreview{}, fmt.Errorf("invalid preview speed")
	}
	surface := lifxdevice.SurfaceFromDevice(d)
	cells := surface.Width * surface.Height
	if surface.Width <= 0 || surface.Height <= 0 || surface.Width > maxEffectPreviewCells || surface.Height > maxEffectPreviewCells || cells > maxEffectPreviewCells {
		return DeviceEffectPreview{}, fmt.Errorf("device surface is too large for local preview")
	}
	effect, err := newAppEffect(req, d, current)
	if err != nil {
		return DeviceEffectPreview{}, err
	}
	step := appEffectStep(req, d)
	state := lifxeffects.NewPhysicalColorState(surface)
	renderer := lifxadapters.NewRendererForDevice(d, func(msg *protocol.Message) error {
		return applyPreviewPacket(&state, surface, msg)
	})
	mask := lifxeffects.NewFrame(surface.Width, surface.Height, step, lifxeffects.Color{Brightness: 100, Kelvin: 3500})
	if err := renderer.RenderFrame(ctx, mask); err != nil {
		return DeviceEffectPreview{}, err
	}
	visible, err := lifxeffects.AdaptPhysicalColorStateToFrame(state, surface, step)
	if err != nil {
		return DeviceEffectPreview{}, err
	}
	result := DeviceEffectPreview{Width: surface.Width, Height: surface.Height, StepMS: step.Milliseconds(), Cells: make([]bool, cells), Frames: [][]HSLColor{}}
	for i, color := range visible.Colors {
		result.Cells[i] = color.Brightness > 0
	}
	state = lifxeffects.NewPhysicalColorState(surface)
	frameCount := min(maxEffectPreviewFrames, maxEffectPreviewColorValues/cells, max(int(max(6*time.Second, time.Duration(req.SpeedMS)*time.Millisecond)/step), 1))
	effect.Reset()
	for i := 0; i < frameCount; i++ {
		if err := ctx.Err(); err != nil {
			return DeviceEffectPreview{}, err
		}
		frame, ok := effect.Next(step)
		if !ok {
			break
		}
		if err := renderer.RenderFrame(ctx, frame); err != nil {
			return DeviceEffectPreview{}, err
		}
		logical, err := lifxeffects.AdaptPhysicalColorStateToFrame(state, surface, step)
		if err != nil {
			return DeviceEffectPreview{}, err
		}
		colors := make([]HSLColor, len(logical.Colors))
		for index, color := range logical.Colors {
			colors[index] = mapLifxColor(color, current.Capability)
		}
		result.Frames = append(result.Frames, colors)
	}
	if len(result.Frames) == 0 {
		return DeviceEffectPreview{}, fmt.Errorf("effect produced no preview frames")
	}
	return result, nil
}

func applyPreviewPacket(state *lifxeffects.PhysicalColorState, surface lifxdevice.Surface, msg *protocol.Message) error {
	switch p := msg.Payload.(type) {
	case *packets.MultiZoneExtendedSetColorZones:
		if int(p.ColorsCount) > len(p.Colors) {
			return fmt.Errorf("invalid preview zone count")
		}
		return state.MergeZoneColors(int(p.Index), p.Colors[:p.ColorsCount])
	case *packets.TileSet64:
		for chain := int(p.TileIndex); chain < int(p.TileIndex)+int(p.Length); chain++ {
			if err := state.MergeMatrixColors(surface, chain, int(p.Rect.X), int(p.Rect.Y), int(p.Rect.Width), p.Colors[:]); err != nil {
				return err
			}
		}
		return nil
	case *packets.TileCopyFrameBuffer:
		// The renderer's entire frame is collected before displaying anything.
		// Buffered writes are therefore already merged into its settled target;
		// preview does not model device transition timing or intermediate buffers.
		return nil
	default:
		return fmt.Errorf("unsupported local preview packet %d", msg.Type())
	}
}
