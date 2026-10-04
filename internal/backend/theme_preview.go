package backend

import (
	"context"
	"fmt"
	"slices"
	"strings"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	lifxadapters "github.com/alessio-palumbo/lifxlan-go/pkg/effects/adapters"
	"github.com/alessio-palumbo/lifxlan-go/pkg/protocol"
	lifxthemes "github.com/alessio-palumbo/lifxlan-go/pkg/themes"
)

const maxThemePreviewTargets = 64

type ThemePreviewer interface {
	PreviewTheme(context.Context, ThemePreviewRequest) (ThemePreview, error)
}

type ThemePreviewRequest struct {
	Theme   lifxthemes.Theme `json:"theme"`
	Serials []string         `json:"serials"`
	// Empty uses PreserveBrightness, unlike the library's palette-mode default.
	Brightness lifxthemes.BrightnessPolicy `json:"brightness,omitempty"`
}

type ThemePreview struct {
	Devices []ThemeDevicePreview `json:"devices"`
}

type ThemeDevicePreview struct {
	Serial string     `json:"serial"`
	Name   string     `json:"name"`
	On     bool       `json:"on"`
	Width  int        `json:"width"`
	Height int        `json:"height"`
	Cells  []bool     `json:"cells"`
	Colors []HSLColor `json:"colors"`
}

func validateThemePreviewRequest(req ThemePreviewRequest) ([]lifxdevice.Serial, error) {
	if err := req.Theme.Validate(); err != nil {
		return nil, err
	}
	if req.Brightness != "" && req.Brightness != lifxthemes.PreserveBrightness && req.Brightness != lifxthemes.PaletteBrightness {
		return nil, fmt.Errorf("unknown theme brightness policy %q", req.Brightness)
	}
	if len(req.Serials) == 0 || len(req.Serials) > maxThemePreviewTargets {
		return nil, fmt.Errorf("theme preview requires 1..%d selected lights", maxThemePreviewTargets)
	}
	serials := make([]lifxdevice.Serial, 0, len(req.Serials))
	seen := make(map[lifxdevice.Serial]bool, len(req.Serials))
	for _, value := range req.Serials {
		serial, err := parseDeviceSerial(Device{Serial: value})
		if err != nil {
			return nil, err
		}
		if serial == (lifxdevice.Serial{}) || seen[serial] {
			return nil, fmt.Errorf("invalid or duplicate theme target %s", serial)
		}
		seen[serial] = true
		serials = append(serials, serial)
	}
	// Use the same ordering for command locks and palette assignment.
	slices.SortFunc(serials, func(a, b lifxdevice.Serial) int { return strings.Compare(a.String(), b.String()) })
	return serials, nil
}

// PreviewTheme may query observed state, but never sends colour/power/effect
// commands. Locks prevent an in-app edit from racing the captured baseline.
func (t *LifxTransport) PreviewTheme(ctx context.Context, req ThemePreviewRequest) (ThemePreview, error) {
	serials, err := validateThemePreviewRequest(req)
	if err != nil {
		return ThemePreview{}, err
	}
	ctrl, err := t.requireController()
	if err != nil {
		return ThemePreview{}, err
	}
	for _, serial := range serials {
		unlock, err := t.lockDeviceCommand(ctx, serial)
		if err != nil {
			return ThemePreview{}, err
		}
		defer unlock()
		d, ok := ctrl.GetDevice(serial)
		if !ok {
			return ThemePreview{}, fmt.Errorf("theme target %s is no longer available", serial)
		}
		if d.Type != lifxdevice.DeviceTypeLight && d.Type != lifxdevice.DeviceTypeHybrid {
			return ThemePreview{}, fmt.Errorf("theme target %s is not a light", serial)
		}
	}
	snapshot, err := ctrl.CaptureStateSnapshot(ctx, serials, lifxcontroller.SnapshotOptions{})
	if err != nil {
		return ThemePreview{}, fmt.Errorf("capture theme state: %w", err)
	}
	if len(snapshot.Devices) != len(serials) {
		return ThemePreview{}, fmt.Errorf("theme snapshot is incomplete")
	}
	states := make(map[lifxdevice.Serial]lifxdevice.DeviceStateSnapshot, len(serials))
	for _, state := range snapshot.Devices {
		if _, duplicate := states[state.Serial]; duplicate {
			return ThemePreview{}, fmt.Errorf("duplicate device in theme snapshot")
		}
		states[state.Serial] = state
	}
	devices := make([]lifxdevice.Device, 0, len(serials))
	for _, serial := range serials {
		state, ok := states[serial]
		if !ok {
			return ThemePreview{}, fmt.Errorf("theme snapshot does not contain %s", serial)
		}
		d, ok := ctrl.GetDevice(serial)
		if !ok {
			return ThemePreview{}, fmt.Errorf("theme target %s disappeared during capture", serial)
		}
		t.mu.RLock()
		current, cached := t.cache[serial.String()]
		if active, ok := t.effects[serial.String()]; ok {
			current, cached = active.previous, true
		}
		t.mu.RUnlock()
		if cached {
			overlayAppEffectState(&state, current)
		}
		devices = append(devices, deviceWithEffectSnapshot(d, state))
	}
	return planThemePreview(ctx, req, devices)
}

// Inputs must contain complete observed state. Planning and preview rendering
// are pure; the same library frames can later be used by the apply path.
func planThemeFrames(ctx context.Context, req ThemePreviewRequest, devices []lifxdevice.Device) ([]lifxthemes.Application, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	serials, err := validateThemePreviewRequest(req)
	if err != nil {
		return nil, err
	}
	if len(devices) != len(serials) {
		return nil, fmt.Errorf("theme target state is incomplete")
	}
	selected := make(map[lifxdevice.Serial]bool, len(serials))
	for _, serial := range serials {
		selected[serial] = true
	}
	for _, d := range devices {
		if !selected[d.Serial] {
			return nil, fmt.Errorf("unexpected theme target %s", d.Serial)
		}
	}
	// The library bounds raw geometry before allocating surfaces. Check the
	// resulting frames against Hikari's smaller interactive-preview budget before
	// converting initial state or producing brightness-preserving frames.
	plan, err := req.Theme.Plan(devices, defaultColorTransitionDuration)
	if err != nil {
		return nil, err
	}
	totalCells := 0
	for _, application := range plan {
		cells := len(application.Frame.Colors)
		if cells > maxEffectPreviewCells || cells > maxEffectPreviewColorValues-totalCells {
			return nil, fmt.Errorf("theme is too large for local preview")
		}
		totalCells += cells
	}
	if req.Brightness == "" || req.Brightness == lifxthemes.PreserveBrightness {
		initial := make(map[lifxdevice.Serial]lifxeffects.Frame, len(devices))
		for _, d := range devices {
			frame, err := lifxeffects.FrameFromDeviceState(d, 0)
			if err != nil {
				return nil, fmt.Errorf("theme initial state %s: %w", d.Serial, err)
			}
			initial[d.Serial] = frame
		}
		plan, err = req.Theme.PlanWithOptions(devices, defaultColorTransitionDuration, lifxthemes.PlanOptions{Brightness: lifxthemes.PreserveBrightness, InitialFrames: initial})
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return plan, nil
}

func planThemePreview(ctx context.Context, req ThemePreviewRequest, devices []lifxdevice.Device) (ThemePreview, error) {
	plan, err := planThemeFrames(ctx, req, devices)
	if err != nil {
		return ThemePreview{}, err
	}
	bySerial := make(map[lifxdevice.Serial]lifxdevice.Device, len(devices))
	for _, d := range devices {
		bySerial[d.Serial] = d
	}
	result := ThemePreview{Devices: make([]ThemeDevicePreview, 0, len(plan))}
	for _, application := range plan {
		if err := ctx.Err(); err != nil {
			return ThemePreview{}, err
		}
		preview, err := renderThemeDevicePreview(ctx, bySerial[application.Serial], application.Frame)
		if err != nil {
			return ThemePreview{}, err
		}
		result.Devices = append(result.Devices, preview)
	}
	return result, nil
}

func renderThemeDevicePreview(ctx context.Context, d lifxdevice.Device, frame lifxeffects.Frame) (ThemeDevicePreview, error) {
	surface := lifxdevice.SurfaceFromDevice(d)
	state := lifxeffects.NewPhysicalColorState(surface)
	renderer := lifxadapters.NewRendererForDevice(d, func(msg *protocol.Message) error { return applyPreviewPacket(&state, surface, msg) })
	mask := lifxeffects.NewFrame(frame.Width, frame.Height, frame.Duration, lifxeffects.Color{Brightness: 100, Kelvin: 3500})
	if err := renderer.RenderFrame(ctx, mask); err != nil {
		return ThemeDevicePreview{}, err
	}
	visible, err := lifxeffects.AdaptPhysicalColorStateToFrame(state, surface, frame.Duration)
	if err != nil {
		return ThemeDevicePreview{}, err
	}
	state = lifxeffects.NewPhysicalColorState(surface)
	if err := renderer.RenderFrame(ctx, frame); err != nil {
		return ThemeDevicePreview{}, err
	}
	logical, err := lifxeffects.AdaptPhysicalColorStateToFrame(state, surface, frame.Duration)
	if err != nil {
		return ThemeDevicePreview{}, err
	}
	preview := ThemeDevicePreview{Serial: d.Serial.String(), Name: d.Label, On: d.PoweredOn, Width: logical.Width, Height: logical.Height, Cells: make([]bool, len(logical.Colors)), Colors: make([]HSLColor, len(logical.Colors))}
	capability := mapLifxDevice(d, "").Capability
	for i, color := range logical.Colors {
		preview.Cells[i] = visible.Colors[i].Brightness > 0
		preview.Colors[i] = mapLifxColor(color, capability)
	}
	return preview, nil
}
