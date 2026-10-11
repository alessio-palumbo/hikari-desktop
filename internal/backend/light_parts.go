package backend

import (
	"context"
	"errors"
	"fmt"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

// Activation normally inherits observed brightness in lifxlan-go. This is only
// the UI fallback when every part has zero stored brightness (percent units).
const lightPartActivationFallbackBrightness = 1.0

type LightPartInfo struct {
	Part       lifxdevice.LightPart `json:"part"`
	ZoneCount  int                  `json:"zoneCount"`
	ZonesKnown bool                 `json:"zonesKnown"`
	Capability DeviceCapability     `json:"capability"`
}

type LightPartState struct {
	Known           bool    `json:"known"`
	On              bool    `json:"on"`
	Brightness      float64 `json:"brightness"`
	DevicePoweredOn bool    `json:"devicePoweredOn"`
}

func devicePoweredOn(device Device) bool {
	if all, ok := device.LightPartStates[lifxdevice.LightPartAll]; ok && all.Known {
		return all.DevicePoweredOn
	}
	return device.On
}

// Power and partial HSBK changes are separate intents. Brightness is a fraction.
type SetLightPartStateRequest struct {
	Serial     string               `json:"serial"`
	Part       lifxdevice.LightPart `json:"part"`
	Power      *bool                `json:"power,omitempty"`
	Hue        *float64             `json:"hue,omitempty"`
	Saturation *float64             `json:"saturation,omitempty"`
	Brightness *float64             `json:"brightness,omitempty"`
	Kelvin     *int                 `json:"kelvin,omitempty"`
}

type lightPartController interface {
	GetLightPartState(lifxdevice.Serial, lifxdevice.LightPart) (lifxcontroller.LightPartState, error)
	SetLightPartColor(context.Context, lifxdevice.Serial, lifxdevice.LightPart, lifxcontroller.LightStateUpdate, lifxcontroller.SnapshotOptions) error
	SetLightPartPower(context.Context, lifxdevice.Serial, lifxdevice.LightPart, bool, lifxcontroller.LightPartPowerOptions) error
}

func mapLightParts(d lifxdevice.Device) []LightPartInfo {
	parts := lifxdevice.LightParts(d)
	result := make([]LightPartInfo, 0, len(parts))
	for _, part := range parts {
		result = append(result, LightPartInfo{
			Part: part.Part, ZoneCount: part.ZoneCount, ZonesKnown: part.ZonesKnown,
			Capability: DeviceCapability{HasColor: part.HasColor, KelvinMin: int(part.TemperatureRange.Min), KelvinMax: int(part.TemperatureRange.Max)},
		})
	}
	return result
}

func (t *LifxTransport) observeLightParts(ctrl lifxController, snapshot *DeviceSnapshot) {
	t.mu.RLock()
	for i := range snapshot.Devices {
		snapshot.Devices[i].EndpointRevision = t.endpointRevision[snapshot.Devices[i].Serial]
	}
	t.mu.RUnlock()
	parts, ok := ctrl.(lightPartController)
	if !ok {
		return
	}
	for i := range snapshot.Devices {
		d := &snapshot.Devices[i]
		if len(d.LightParts) < 2 {
			continue
		}
		serial, err := parseDeviceSerial(*d)
		if err != nil {
			continue
		}
		d.LightPartStates = make(map[lifxdevice.LightPart]LightPartState)
		for _, part := range []lifxdevice.LightPart{lifxdevice.LightPartAll, lifxdevice.LightPartMain, lifxdevice.LightPartUplight} {
			state, err := parts.GetLightPartState(serial, part)
			if err == nil {
				d.LightPartStates[part] = LightPartState{Known: state.Known, On: state.On, Brightness: state.Brightness / 100, DevicePoweredOn: state.DevicePoweredOn}
			}
		}
		if all := d.LightPartStates[lifxdevice.LightPartAll]; all.Known {
			d.On = all.On
			d.Brightness = all.Brightness
		}
	}
}

func (t *LifxTransport) SetLightPartState(ctx context.Context, req SetLightPartStateRequest) error {
	ctrl, err := t.requireController()
	if err != nil {
		return err
	}
	serial, err := lifxdevice.SerialFromHex(req.Serial)
	if err != nil {
		return fmt.Errorf("invalid device serial: %w", err)
	}
	unlock, err := t.lockDeviceCommand(ctx, serial)
	if err != nil {
		return err
	}
	defer unlock()
	return sendLightPartState(ctx, ctrl, serial, req)
}

func sendLightPartState(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial, req SetLightPartStateRequest) error {
	parts, ok := ctrl.(lightPartController)
	if !ok {
		return fmt.Errorf("light part controls are unavailable")
	}
	hasColor := req.Hue != nil || req.Saturation != nil || req.Brightness != nil || req.Kelvin != nil
	if req.Power != nil && hasColor || req.Power == nil && !hasColor {
		return fmt.Errorf("choose either power or color changes")
	}
	if req.Power != nil {
		return lightPartSendError(parts.SetLightPartPower(ctx, serial, req.Part, *req.Power, lightPartPowerOptions(*req.Power)))
	}
	update := lifxcontroller.LightStateUpdate{Hue: req.Hue, Saturation: req.Saturation, Duration: defaultColorTransitionDuration}
	if req.Brightness != nil {
		brightness := *req.Brightness * 100
		update.Brightness = &brightness
	}
	if req.Kelvin != nil {
		if *req.Kelvin < 1 || *req.Kelvin > 65535 {
			return fmt.Errorf("invalid white temperature")
		}
		kelvin := uint16(*req.Kelvin)
		update.Kelvin = &kelvin
	}
	activate := false
	if req.Hue != nil || req.Saturation != nil || req.Kelvin != nil || req.Brightness != nil && *req.Brightness > 0 {
		state, err := parts.GetLightPartState(serial, req.Part)
		if err != nil {
			return err
		}
		if !state.Known {
			if _, err := ctrl.CaptureStateSnapshot(ctx, []lifxdevice.Serial{serial}, lifxcontroller.SnapshotOptions{}); err != nil {
				return fmt.Errorf("observe before light part activation: %w", err)
			}
			state, err = parts.GetLightPartState(serial, req.Part)
			if err != nil {
				return err
			}
			if !state.Known {
				return fmt.Errorf("light part state is not known yet")
			}
		}
		activate = !state.On
		if activate {
			// Prepare invisible state immediately, so activation's fresh-state
			// merge cannot sample an in-progress color transition.
			update.Duration = 0
		}
	}
	if err := parts.SetLightPartColor(ctx, serial, req.Part, update, lifxcontroller.SnapshotOptions{}); err != nil {
		return lightPartSendError(err)
	}
	if activate {
		if err := parts.SetLightPartPower(ctx, serial, req.Part, true, lightPartPowerOptions(true)); err != nil {
			return fmt.Errorf("color/brightness update sent but activation failed; device may be partially updated: %w", lightPartSendError(err))
		}
	}
	return nil
}

func lightPartPowerOptions(on bool) lifxcontroller.LightPartPowerOptions {
	options := lifxcontroller.LightPartPowerOptions{Duration: defaultColorTransitionDuration}
	if on {
		fallback := lightPartActivationFallbackBrightness
		options.FallbackBrightness = &fallback
	}
	return options
}

func lightPartSendError(err error) error {
	if err == nil {
		return nil // Successful sends are not acknowledgements; observations own UI state.
	}
	var partial *lifxcontroller.StateSendError
	if errors.As(err, &partial) {
		return fmt.Errorf("device may be partially updated (%d/%d messages sent); check its observed state: %w", partial.Sent, partial.Total, err)
	}
	if errors.Is(err, lifxcontroller.ErrEndpointChanged) {
		return fmt.Errorf("device address changed; wait for fresh state before trying again: %w", err)
	}
	return err
}
