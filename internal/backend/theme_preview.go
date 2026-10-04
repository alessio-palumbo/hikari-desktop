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
	PreviewTheme(context.Context, ThemeRequest) (ThemePreview, error)
}

type ThemeRequest struct {
	Theme   lifxthemes.Theme `json:"theme"`
	Serials []string         `json:"serials"`
	// Empty uses PreserveBrightness, unlike the library's palette-mode default.
	Brightness lifxthemes.BrightnessPolicy `json:"brightness,omitempty"`
	// Runtime-only options shared by preview and application, not saved palettes.
	Variation    uint32                  `json:"variation,omitempty"`
	Seed         uint32                  `json:"seed,omitempty"`
	Reverse      bool                    `json:"reverse,omitempty"`
	MatrixLayout lifxthemes.MatrixLayout `json:"matrixLayout,omitempty"`
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

func validateThemeRequest(req ThemeRequest) ([]lifxdevice.Serial, error) {
	if err := req.Theme.Validate(); err != nil {
		return nil, err
	}
	if req.Brightness != "" && req.Brightness != lifxthemes.PreserveBrightness && req.Brightness != lifxthemes.PaletteBrightness {
		return nil, fmt.Errorf("unknown theme brightness policy %q", req.Brightness)
	}
	if req.MatrixLayout != "" && req.MatrixLayout != lifxthemes.MatrixThemeLayout && req.MatrixLayout != lifxthemes.MatrixSpatial {
		return nil, fmt.Errorf("unknown matrix theme layout %q", req.MatrixLayout)
	}
	if len(req.Serials) == 0 || len(req.Serials) > maxThemePreviewTargets {
		return nil, fmt.Errorf("theme requires 1..%d selected lights", maxThemePreviewTargets)
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
func (t *LifxTransport) PreviewTheme(ctx context.Context, req ThemeRequest) (ThemePreview, error) {
	prepared, err := t.prepareTheme(ctx, req)
	if err != nil {
		return ThemePreview{}, err
	}
	defer prepared.release()
	return previewThemeFrames(ctx, prepared.plan, prepared.devices)
}

type preparedTheme struct {
	controller lifxController
	devices    []lifxdevice.Device
	plan       []lifxthemes.Application
	release    func()
}

// Keep target locks until the caller finishes previewing or sending this plan.
func (t *LifxTransport) prepareTheme(ctx context.Context, req ThemeRequest) (preparedTheme, error) {
	serials, err := validateThemeRequest(req)
	if err != nil {
		return preparedTheme{}, err
	}
	ctrl, err := t.requireController()
	if err != nil {
		return preparedTheme{}, err
	}
	var releases []func()
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	ready := false
	defer func() {
		if !ready {
			release()
		}
	}()
	for _, serial := range serials {
		unlock, err := t.lockDeviceCommand(ctx, serial)
		if err != nil {
			return preparedTheme{}, err
		}
		releases = append(releases, unlock)
		d, ok := ctrl.GetDevice(serial)
		if !ok {
			return preparedTheme{}, fmt.Errorf("theme target %s is no longer available", serial)
		}
		if d.Type != lifxdevice.DeviceTypeLight && d.Type != lifxdevice.DeviceTypeHybrid {
			return preparedTheme{}, fmt.Errorf("theme target %s is not a light", serial)
		}
		if cached := t.cachedDevice(serial.String()); cached != nil && !cached.Online {
			return preparedTheme{}, fmt.Errorf("theme target %s is offline", serial)
		}
	}
	snapshot, err := ctrl.CaptureStateSnapshot(ctx, serials, lifxcontroller.SnapshotOptions{})
	if err != nil {
		return preparedTheme{}, fmt.Errorf("capture theme state: %w", err)
	}
	if len(snapshot.Devices) != len(serials) {
		return preparedTheme{}, fmt.Errorf("theme snapshot is incomplete")
	}
	states := make(map[lifxdevice.Serial]lifxdevice.DeviceStateSnapshot, len(serials))
	for _, state := range snapshot.Devices {
		if _, duplicate := states[state.Serial]; duplicate {
			return preparedTheme{}, fmt.Errorf("duplicate device in theme snapshot")
		}
		states[state.Serial] = state
	}
	devices := make([]lifxdevice.Device, 0, len(serials))
	for _, serial := range serials {
		state, ok := states[serial]
		if !ok {
			return preparedTheme{}, fmt.Errorf("theme snapshot does not contain %s", serial)
		}
		d, ok := ctrl.GetDevice(serial)
		if !ok {
			return preparedTheme{}, fmt.Errorf("theme target %s disappeared during capture", serial)
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
	plan, err := planThemeFrames(ctx, req, devices)
	if err != nil {
		return preparedTheme{}, err
	}
	ready = true
	return preparedTheme{controller: ctrl, devices: devices, plan: plan, release: release}, nil
}

// Inputs must contain complete observed state. Planning and preview rendering
// are pure; the resulting library frames are shared by preview and application.
func planThemeFrames(ctx context.Context, req ThemeRequest, devices []lifxdevice.Device) ([]lifxthemes.Application, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	serials, err := validateThemeRequest(req)
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
	options := lifxthemes.PlanOptions{Variation: uint64(req.Variation), Seed: uint64(req.Seed), Reverse: req.Reverse, MatrixLayout: req.MatrixLayout}
	plan, err := req.Theme.PlanWithOptions(devices, defaultColorTransitionDuration, options)
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
		options.Brightness, options.InitialFrames = lifxthemes.PreserveBrightness, initial
		plan, err = req.Theme.PlanWithOptions(devices, defaultColorTransitionDuration, options)
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return plan, nil
}

func planThemePreview(ctx context.Context, req ThemeRequest, devices []lifxdevice.Device) (ThemePreview, error) {
	plan, err := planThemeFrames(ctx, req, devices)
	if err != nil {
		return ThemePreview{}, err
	}
	return previewThemeFrames(ctx, plan, devices)
}

func previewThemeFrames(ctx context.Context, plan []lifxthemes.Application, devices []lifxdevice.Device) (ThemePreview, error) {
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
