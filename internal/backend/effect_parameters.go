package backend

import (
	"fmt"
	"math"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
)

type EffectParameterChoice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type EffectParameter struct {
	Key         string                  `json:"key"`
	Label       string                  `json:"label"`
	Description string                  `json:"description,omitempty"`
	Unit        string                  `json:"unit,omitempty"`
	Min         float64                 `json:"min"`
	Max         float64                 `json:"max"`
	Step        float64                 `json:"step"`
	Kind        string                  `json:"kind"`
	Choices     []EffectParameterChoice `json:"choices,omitempty"`
	Default     any                     `json:"default"`
	Value       any                     `json:"value"`
}

type DeviceEffectParameterSource interface {
	EffectParameters(string, DeviceEffect) ([]EffectParameter, error)
}

// Expose only useful numeric and choice controls. Palettes and timing
// retain Hikari's existing presets; the library owns parameter validation.
func configurableEffectDefaults(effect DeviceEffect) map[string]any {
	switch effect {
	case DeviceEffectFlow:
		return map[string]any{"direction": "forward", "axis": "diagonal"}
	case DeviceEffectFrames:
		return map[string]any{"direction": "in_out"}
	case DeviceEffectSnake, DeviceEffectWorm:
		return map[string]any{"size": defaultAppEffectTailSize}
	case DeviceEffectWave:
		return map[string]any{"waves": 2, "amplitude": 2, "width": 3}
	case DeviceEffectRing:
		return map[string]any{"width": 1.6, "floor": .22}
	case DeviceEffectComet:
		return map[string]any{"tail_size": 5, "background_brightness_factor": 1, "peak_brightness_factor": 1.5, "tail_curve": 3, "tail_saturation_factor": .25}
	case DeviceEffectSparkle:
		return map[string]any{"density": .18, "background_floor": .55, "peak_brightness_factor": 1.5}
	case DeviceEffectScanner:
		return map[string]any{"background_brightness_factor": .8, "peak_brightness_factor": 1.55}
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
		if param.Kind == lifxeffects.ParamChoiceKind {
			choices := make([]EffectParameterChoice, 0, len(param.Choices))
			for _, choice := range param.Choices {
				label := choice.Label
				if effect == DeviceEffectFrames {
					switch choice.Value {
					case "in_out":
						label = "In then out"
					case "out_in":
						label = "Out then in"
					}
				}
				choices = append(choices, EffectParameterChoice{Value: choice.Value, Label: label})
			}
			parameters = append(parameters, EffectParameter{Key: param.Key, Label: "direction", Kind: "choice", Choices: choices, Default: value, Value: value})
			if param.Key == "axis" {
				parameters[len(parameters)-1].Label = "axis"
			}
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
		number, ok := effectParameterNumber(value)
		if !ok {
			return nil, fmt.Errorf("invalid numeric default %q", param.Key)
		}
		parameters = append(parameters, EffectParameter{Key: param.Key, Label: label, Description: description, Unit: unit, Kind: "number", Min: *param.Min, Max: maximum, Step: *param.Step, Default: number, Value: number})
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
	if effect == DeviceEffectFlow && caps.LightType == lifxdevice.LightTypeMultiZone {
		filtered := parameters[:0]
		for _, parameter := range parameters {
			if parameter.Key != "axis" {
				filtered = append(filtered, parameter)
			}
		}
		parameters = filtered
	}
	for i := range parameters {
		param := &parameters[i]
		switch param.Key {
		case "size":
			param.Max = float64(max(caps.Width, 1))
			param.Default = float64(appEffectSnakeSize(d))
		case "tail_size":
			number, _ := effectParameterNumber(param.Default)
			param.Max = max(number, float64(caps.Width))
		case "amplitude":
			number, _ := effectParameterNumber(param.Default)
			param.Max = max(number, float64(caps.Height-1))
		case "width":
			number, _ := effectParameterNumber(param.Default)
			param.Max = max(number, float64(max(caps.Width, caps.Height)))
		}
		param.Value = param.Default
	}
	return parameters, nil
}

func newConfigurableAppEffect(req StartDeviceEffectRequest, d lifxdevice.Device, current Device) (lifxeffects.Effect, error) {
	definitions, err := effectParametersForDevice(req.Effect, d)
	if err != nil {
		return nil, err
	}
	params := map[string]any{}
	for _, definition := range definitions {
		params[definition.Key] = definition.Default
	}
	for key, value := range req.Params {
		if _, ok := params[key]; !ok {
			return nil, fmt.Errorf("unsupported effect parameter %q", key)
		}
		params[key] = value
	}
	for _, definition := range definitions {
		if err := validateEffectParameter(definition, params[definition.Key], true); err != nil {
			return nil, err
		}
	}
	switch req.Effect {
	case DeviceEffectFlow:
		params["palette"] = appEffectFlowPalette(current)
		if _, ok := params["axis"]; !ok {
			params["axis"] = string(lifxeffects.FlowAxisDiagonal)
		}
		params["brightness_mode"] = string(lifxeffects.FlowBrightnessConstant)
		params["sampling"] = string(lifxeffects.FlowSamplingInterpolate)
		params["period"] = appEffectPeriod(req.SpeedMS, 4*time.Second)
	case DeviceEffectFrames:
		params["palette"] = lifxeffects.Palette{Base: appEffectPalette(current)}
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

func effectParameterNumber(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case int:
		return float64(number), true
	default:
		return 0, false
	}
}

func validateEffectParameter(parameter EffectParameter, value any, bounded bool) error {
	if parameter.Kind == "choice" {
		choice, ok := value.(string)
		if ok {
			for _, option := range parameter.Choices {
				if choice == option.Value {
					return nil
				}
			}
		}
	} else if number, ok := effectParameterNumber(value); ok && !math.IsNaN(number) && !math.IsInf(number, 0) && number >= parameter.Min && (!bounded || number <= parameter.Max) && (parameter.Step != 1 || number == math.Trunc(number)) {
		return nil
	}
	return fmt.Errorf("invalid effect parameter %q", parameter.Key)
}
