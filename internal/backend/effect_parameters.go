package backend

import (
	"fmt"
	"math"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
)

type EffectParameter struct {
	Key         string  `json:"key"`
	Label       string  `json:"label"`
	Description string  `json:"description,omitempty"`
	Unit        string  `json:"unit,omitempty"`
	Min         float64 `json:"min"`
	Max         float64 `json:"max"`
	Step        float64 `json:"step"`
	Default     float64 `json:"default"`
	Value       float64 `json:"value"`
}

type DeviceEffectParameterSource interface {
	EffectParameters(string, DeviceEffect) ([]EffectParameter, error)
}

// This initial UI exposes only bounded numeric controls. Palettes and timing
// retain Hikari's existing presets; the library owns parameter validation.
func configurableEffectDefaults(effect DeviceEffect) map[string]float64 {
	switch effect {
	case DeviceEffectSnake, DeviceEffectWorm:
		return map[string]float64{"size": defaultAppEffectTailSize}
	case DeviceEffectWave:
		return map[string]float64{"waves": 2, "amplitude": 2, "width": 3}
	case DeviceEffectRing:
		return map[string]float64{"width": 1.6, "floor": .22}
	case DeviceEffectComet:
		return map[string]float64{"tail_size": 5, "background_brightness_factor": 1, "peak_brightness_factor": 1.5, "tail_curve": 3, "tail_saturation_factor": .25}
	case DeviceEffectSparkle:
		return map[string]float64{"density": .18, "background_floor": .55, "peak_brightness_factor": 1.5}
	case DeviceEffectScanner:
		return map[string]float64{"background_brightness_factor": .8, "peak_brightness_factor": 1.55}
	default:
		return nil
	}
}

func EffectParameterDefinitions(effect DeviceEffect) ([]EffectParameter, error) {
	defaults := configurableEffectDefaults(effect)
	if defaults == nil {
		return nil, fmt.Errorf("effect has no configurable controls")
	}
	definition, ok := lifxeffects.Definition(lifxeffects.EffectID(effect))
	if !ok {
		return nil, fmt.Errorf("effect is not registered")
	}
	parameters := []EffectParameter{}
	for _, param := range definition.Params {
		value, exposed := defaults[param.Key]
		if !exposed {
			continue
		}
		if param.Kind != lifxeffects.ParamNumber || param.Min == nil || param.Step == nil {
			return nil, fmt.Errorf("unsupported effect parameter %q", param.Key)
		}
		label := param.Label
		description := ""
		unit := ""
		// Unbounded library sizes still need a finite slider range. Real devices
		// refine these fallback ranges using their logical surface dimensions.
		maximum := 64.0
		if param.Max != nil {
			maximum = *param.Max
		}
		switch param.Key {
		case "background_floor", "background_brightness_factor", "floor":
			label = "background brightness"
			unit = "%"
			description = "Brightness outside the animated highlight, relative to palette brightness. 50% means half as bright."
		case "peak_brightness_factor":
			label = "peak brightness"
			unit = "%"
			description = "Maximum animated highlight brightness, relative to palette brightness. 150% means 1.5 times as bright, capped at 100% device brightness."
		case "density":
			label = "density"
			unit = "%"
		case "size":
			label, unit = "length", "cells"
			description = "Trail length in logical cells, limited to the matrix width."
		case "tail_size":
			label, unit = "tail length", "cells"
		case "tail_curve":
			label = "tail falloff"
			description = "Higher values make the tail fade toward the background faster."
		case "tail_saturation_factor":
			label, unit = "tail saturation", "%"
			description = "Saturation retained at the end of the tail, relative to its colour."
		case "waves":
			label, maximum = "wave count", 8
		case "amplitude":
			label, unit = "wave height", "cells"
		case "width":
			label, unit = "width", "cells"
			if effect == DeviceEffectRing {
				label = "thickness"
			}
		}
		parameters = append(parameters, EffectParameter{Key: param.Key, Label: label, Description: description, Unit: unit, Min: *param.Min, Max: maximum, Step: *param.Step, Default: value, Value: value})
	}
	if len(parameters) != len(defaults) {
		return nil, fmt.Errorf("effect parameter schema is incompatible")
	}
	return parameters, nil
}

func (t *LifxTransport) EffectParameters(serial string, effect DeviceEffect) ([]EffectParameter, error) {
	ctrl, err := t.requireController()
	if err != nil {
		return nil, err
	}
	id, err := parseDeviceSerial(Device{Serial: serial})
	if err != nil {
		return nil, err
	}
	d, ok := ctrl.GetDevice(id)
	if !ok {
		return nil, fmt.Errorf("device is no longer available")
	}
	parameters, err := effectParametersForDevice(effect, d)
	if err != nil {
		return nil, err
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	if active, ok := t.effects[serial]; ok && active.effect == effect {
		for i := range parameters {
			if value, ok := active.params[parameters[i].Key]; ok {
				parameters[i].Value = value
			}
		}
	}
	return parameters, nil
}

func effectParametersForDevice(effect DeviceEffect, d lifxdevice.Device) ([]EffectParameter, error) {
	parameters, err := EffectParameterDefinitions(effect)
	if err != nil {
		return nil, err
	}
	caps := appEffectCapabilities(d)
	for i := range parameters {
		param := &parameters[i]
		switch param.Key {
		case "size":
			param.Max = float64(max(caps.Width, 1))
			param.Default = float64(appEffectSnakeSize(d))
		case "tail_size":
			param.Max = max(param.Default, float64(caps.Width))
		case "amplitude":
			param.Max = max(param.Default, float64(caps.Height-1))
		case "width":
			param.Max = max(param.Default, float64(max(caps.Width, caps.Height)))
		}
		param.Value = param.Default
	}
	return parameters, nil
}

func newConfigurableAppEffect(req StartDeviceEffectRequest, d lifxdevice.Device, current Device) (lifxeffects.Effect, error) {
	defaults := configurableEffectDefaults(req.Effect)
	definitions, err := effectParametersForDevice(req.Effect, d)
	if err != nil {
		return nil, err
	}
	params := map[string]any{}
	for _, definition := range definitions {
		params[definition.Key] = definition.Default
	}
	for key, value := range req.Params {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("effect parameter %q must be finite", key)
		}
		if _, ok := defaults[key]; !ok {
			return nil, fmt.Errorf("unsupported effect parameter %q", key)
		}
		params[key] = value
	}
	for _, definition := range definitions {
		value := params[definition.Key].(float64)
		if value < definition.Min || value > definition.Max || (definition.Step == 1 && value != math.Trunc(value)) {
			return nil, fmt.Errorf("invalid effect parameter %q", definition.Key)
		}
	}
	switch req.Effect {
	case DeviceEffectSnake, DeviceEffectWorm:
		params["color"] = appEffectPrimaryColor(current)
	case DeviceEffectWave:
		params["palette"] = appEffectFlowPalette(current)
	case DeviceEffectRing:
		params["palette"] = appEffectFlowPalette(current)
		params["period"] = appEffectPeriod(req.SpeedMS, 2*time.Second)
	case DeviceEffectComet:
		params["palette"] = appEffectCometPalette(current)
		params["axis"] = string(lifxeffects.FlowAxisHorizontal)
		params["period"] = appEffectPeriod(req.SpeedMS, 4*time.Second)
	case DeviceEffectSparkle:
		params["palette"] = appEffectSparklePalette(current)
		params["period"] = appEffectPeriod(req.SpeedMS, 2*time.Second)
		params["decay"], params["seed"] = 1.2, 1
	case DeviceEffectScanner:
		params["palette"] = appEffectScannerPalette(current)
		params["period"] = appEffectPeriod(req.SpeedMS, appEffectScannerPeriod(req.Device.Kind))
		params["axis"] = string(lifxeffects.FlowAxisHorizontal)
	}
	return lifxeffects.New(lifxeffects.Config{ID: lifxeffects.EffectID(req.Effect), Params: params}, appEffectCapabilities(d))
}
