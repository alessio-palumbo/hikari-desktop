package backend

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const lifxLabelMaxBytes = 32

func resolveDeviceMetadata(snapshot DeviceSnapshot, req SetDeviceMetadataRequest) (Device, Location, Group, string, error) {
	label := strings.TrimSpace(req.Label)
	if err := validateLifxLabel(label); err != nil {
		return Device{}, Location{}, Group{}, "", err
	}

	var selected Device
	foundDevice := false
	for _, device := range snapshot.Devices {
		if device.Serial == req.Serial {
			selected = device
			foundDevice = true
			break
		}
	}
	if !foundDevice {
		return Device{}, Location{}, Group{}, "", fmt.Errorf("device %q is not currently available", req.Serial)
	}

	var location Location
	foundLocation := false
	for _, candidate := range snapshot.Locations {
		if candidate.ID == req.LocationID {
			location = candidate
			foundLocation = true
			break
		}
	}
	if !foundLocation {
		return Device{}, Location{}, Group{}, "", fmt.Errorf("location %q is not currently available", req.LocationID)
	}

	var group Group
	foundGroup := false
	for _, candidate := range snapshot.Groups {
		if candidate.ID == req.GroupID {
			group = candidate
			foundGroup = true
			break
		}
	}
	if !foundGroup {
		return Device{}, Location{}, Group{}, "", fmt.Errorf("group %q is not currently available", req.GroupID)
	}
	if group.LocationID != location.ID {
		return Device{}, Location{}, Group{}, "", fmt.Errorf("group %q does not belong to location %q", group.Name, location.Name)
	}

	return selected, location, group, label, nil
}

func validateLifxLabel(label string) error {
	if label == "" {
		return fmt.Errorf("device label is required")
	}
	if !utf8.ValidString(label) {
		return fmt.Errorf("device label is not valid UTF-8")
	}
	if strings.IndexByte(label, 0) >= 0 {
		return fmt.Errorf("device label contains a null byte")
	}
	if len(label) > lifxLabelMaxBytes {
		return fmt.Errorf("device label is %d bytes; maximum is %d", len(label), lifxLabelMaxBytes)
	}
	return nil
}
