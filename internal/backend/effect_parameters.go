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
		if param.Kind != lifxeffects.ParamNumber || param.Min == nil || param.Max == nil || param.Step == nil {
			return nil, fmt.Errorf("unsupported effect parameter %q", param.Key)
		}
		label := param.Label
		description := ""
		switch param.Key {
		case "background_floor", "background_brightness_factor":
			label = "background brightness"
			description = "Brightness outside the animated highlight, relative to palette brightness. 50% means half as bright."
		case "peak_brightness_factor":
			label = "peak brightness"
			description = "Maximum animated highlight brightness, relative to palette brightness. 150% means 1.5 times as bright, capped at 100% device brightness."
		case "density":
			label = "density"
		}
		parameters = append(parameters, EffectParameter{Key: param.Key, Label: label, Description: description, Min: *param.Min, Max: *param.Max, Step: *param.Step, Default: value, Value: value})
	}
	if len(parameters) != len(defaults) {
		return nil, fmt.Errorf("effect parameter schema is incompatible")
	}
	return parameters, nil
}

func (t *LifxTransport) EffectParameters(serial string, effect DeviceEffect) ([]EffectParameter, error) {
	parameters, err := EffectParameterDefinitions(effect)
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

func newConfigurableAppEffect(req StartDeviceEffectRequest, d lifxdevice.Device, current Device) (lifxeffects.Effect, error) {
	defaults := configurableEffectDefaults(req.Effect)
	params := map[string]any{}
	for key, value := range defaults {
		params[key] = value
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
	switch req.Effect {
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
