package backend

import (
	"fmt"
	"math"

	commandclient "github.com/alessio-palumbo/lifx-command-engine/client"
)

func CommandSnapshotFromDeviceSnapshot(snapshot DeviceSnapshot) commandclient.DeviceSnapshot {
	groupsByID := make(map[string]Group, len(snapshot.Groups))
	locationsByID := make(map[string]Location, len(snapshot.Locations))
	for _, location := range snapshot.Locations {
		locationsByID[location.ID] = location
	}
	for _, group := range snapshot.Groups {
		groupsByID[group.ID] = group
	}

	locations := make([]commandclient.NamedRef, 0, len(snapshot.Locations))
	for _, location := range snapshot.Locations {
		locations = append(locations, commandclient.NamedRef{ID: location.ID, Label: location.Name})
	}
	groups := make([]commandclient.NamedRef, 0, len(snapshot.Groups))
	for _, group := range snapshot.Groups {
		groups = append(groups, commandclient.NamedRef{ID: group.ID, Label: group.Name})
	}
	devices := make([]commandclient.SnapshotDevice, 0, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		group := groupsByID[device.GroupID]
		location := locationsByID[group.LocationID]
		mapped := commandclient.SnapshotDevice{
			Serial:    device.Serial,
			Label:     device.Name,
			Group:     group.Name,
			Location:  location.Name,
			ProductID: device.ProductID,
			HasColor:  device.Capability.HasColor,
		}
		if device.Capability.KelvinMin > 0 {
			mapped.MinKelvin = uint16(device.Capability.KelvinMin)
		}
		if device.Capability.KelvinMax > 0 {
			mapped.MaxKelvin = uint16(device.Capability.KelvinMax)
		}
		devices = append(devices, mapped)
	}
	return commandclient.DeviceSnapshot{Locations: locations, Groups: groups, Devices: devices}
}

func commandPreviewFromPlan(plan commandclient.CommandPlan, snapshot DeviceSnapshot) (CommandPreview, error) {
	if plan.SchemaVersion != commandEnginePlanSchema {
		return CommandPreview{}, fmt.Errorf("unsupported command plan schema %q", plan.SchemaVersion)
	}
	preview := CommandPreview{
		Summary:           plan.Summary,
		Confidence:        plan.Confidence,
		ConfidenceLevel:   plan.ConfidenceResult.Level,
		Reasons:           append([]string(nil), plan.ConfidenceResult.Reasons...),
		NeedsConfirmation: plan.NeedsConfirmation,
		Empty:             len(plan.Commands) == 0,
		Commands:          make([]CommandPreviewCommand, 0, len(plan.Commands)),
	}
	devicesBySerial := make(map[string]Device, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		devicesBySerial[device.Serial] = device
	}
	for _, command := range plan.Commands {
		targets := make([]CommandPreviewTarget, 0, len(command.Targets))
		for _, target := range command.Targets {
			device, ok := devicesBySerial[target.Serial]
			if !ok {
				return CommandPreview{}, fmt.Errorf("command targets unknown device %q", target.Serial)
			}
			if device.Kind == DeviceKindSwitch {
				return CommandPreview{}, fmt.Errorf("text commands currently support lights only")
			}
			if err := validateCommandAction(command.Action, device); err != nil {
				return CommandPreview{}, err
			}
			targets = append(targets, CommandPreviewTarget{
				Serial:   target.Serial,
				Label:    firstNonEmpty(target.Label, device.Name),
				Group:    target.Group,
				Location: target.Location,
			})
		}
		preview.Commands = append(preview.Commands, CommandPreviewCommand{
			Targets: targets,
			Action:  commandPreviewAction(command.Action),
		})
	}
	return preview, nil
}

func validateCommandAction(action commandclient.Action, device Device) error {
	if action.Hue != nil && (*action.Hue < 0 || *action.Hue > 360 || math.IsNaN(*action.Hue)) {
		return fmt.Errorf("command hue %.1f is outside 0-360", *action.Hue)
	}
	if action.Saturation != nil && (*action.Saturation < 0 || *action.Saturation > 100 || math.IsNaN(*action.Saturation)) {
		return fmt.Errorf("command saturation %.1f is outside 0-100", *action.Saturation)
	}
	if action.Brightness != nil && (*action.Brightness < 0 || *action.Brightness > 100 || math.IsNaN(*action.Brightness)) {
		return fmt.Errorf("command brightness %.1f is outside 0-100", *action.Brightness)
	}
	if (action.Hue != nil || action.Saturation != nil) && !device.Capability.HasColor {
		return fmt.Errorf("%s does not support color", device.Name)
	}
	if action.Kelvin != nil {
		kelvin := int(*action.Kelvin)
		if device.Capability.KelvinMin > 0 && kelvin < device.Capability.KelvinMin {
			return fmt.Errorf("command Kelvin %d is below %s minimum %d", kelvin, device.Name, device.Capability.KelvinMin)
		}
		if device.Capability.KelvinMax > 0 && kelvin > device.Capability.KelvinMax {
			return fmt.Errorf("command Kelvin %d is above %s maximum %d", kelvin, device.Name, device.Capability.KelvinMax)
		}
	}
	return nil
}

func commandPreviewAction(action commandclient.Action) CommandPreviewAction {
	preview := CommandPreviewAction{
		Power:      action.Power,
		Hue:        action.Hue,
		Saturation: action.Saturation,
		Brightness: action.Brightness,
	}
	if action.Kelvin != nil {
		kelvin := int(*action.Kelvin)
		preview.Kelvin = &kelvin
	}
	if action.DurationMS != nil {
		duration := int(*action.DurationMS)
		preview.DurationMS = &duration
	}
	return preview
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
