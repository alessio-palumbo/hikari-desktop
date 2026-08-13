package backend

import (
	"math"
	"testing"
)

func TestCommandSnapshotFromDeviceSnapshotMapsInventory(t *testing.T) {
	snapshot := DeviceSnapshot{
		Locations: []Location{{ID: "home", Name: "Home"}},
		Groups:    []Group{{ID: "desk", LocationID: "home", Name: "Desk"}},
		Devices: []Device{{
			GroupID:    "desk",
			Serial:     "d0:73:d5:01:a2:c3",
			Name:       "Moon",
			Kind:       DeviceKindMatrix,
			ProductID:  201,
			On:         true,
			Brightness: 0.42,
			Kelvin:     3500,
			Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
			Chain: []Matrix{{Pixels: []HSLColor{
				{H: 20, S: 0.2, L: 0.4, Kelvin: 3000},
				{H: 40, S: 0.4, L: 0.6, Kelvin: 5000},
			}}},
		}},
	}

	got := CommandSnapshotFromDeviceSnapshot(snapshot)

	if len(got.Locations) != 1 || got.Locations[0].ID != "home" || got.Locations[0].Label != "Home" {
		t.Fatalf("locations = %#v", got.Locations)
	}
	if len(got.Groups) != 1 || got.Groups[0].ID != "desk" || got.Groups[0].Label != "Desk" {
		t.Fatalf("groups = %#v", got.Groups)
	}
	if len(got.Devices) != 1 {
		t.Fatalf("devices = %#v", got.Devices)
	}
	device := got.Devices[0]
	if device.Serial != "d0:73:d5:01:a2:c3" || device.Label != "Moon" || device.Group != "Desk" || device.Location != "Home" {
		t.Fatalf("device identity = %#v", device)
	}
	if !device.HasColor || device.MinKelvin != 1500 || device.MaxKelvin != 9000 || device.ProductID != 201 {
		t.Fatalf("device capability = %#v", device)
	}
	state := device.CurrentState
	if state == nil || state.Power == nil || !*state.Power || state.Brightness == nil || *state.Brightness != 42 {
		t.Fatalf("current power/brightness = %#v", state)
	}
	if state.Hue == nil || !nearFloat(*state.Hue, 30) || state.Saturation == nil || !nearFloat(*state.Saturation, 30) {
		t.Fatalf("current color = %#v", state)
	}
	if state.Kelvin == nil || *state.Kelvin != 4000 {
		t.Fatalf("current kelvin = %#v", state)
	}
}

func TestCommandSnapshotFromDeviceSnapshotHandlesEmptyInventory(t *testing.T) {
	got := CommandSnapshotFromDeviceSnapshot(emptyDeviceSnapshot())
	if len(got.Locations) != 0 || len(got.Groups) != 0 || len(got.Devices) != 0 {
		t.Fatalf("snapshot = %#v, want empty slices", got)
	}
}

func TestCommandSnapshotFromDeviceSnapshotSkipsSwitches(t *testing.T) {
	snapshot := DeviceSnapshot{
		Locations: []Location{{ID: "home", Name: "Home"}},
		Groups:    []Group{{ID: "desk", LocationID: "home", Name: "Desk"}},
		Devices: []Device{
			{GroupID: "desk", Serial: "light", Name: "Desk Lamp", Kind: DeviceKindSingle},
			{GroupID: "desk", Serial: "switch", Name: "Desk Switch", Kind: DeviceKindSwitch},
		},
	}

	got := CommandSnapshotFromDeviceSnapshot(snapshot)

	if len(got.Devices) != 1 || got.Devices[0].Serial != "light" {
		t.Fatalf("devices = %#v, want only light target", got.Devices)
	}
}

func TestCommandSnapshotFromDeviceSnapshotMapsSingleZoneCurrentState(t *testing.T) {
	snapshot := DeviceSnapshot{
		Locations: []Location{{ID: "home", Name: "Home"}},
		Groups:    []Group{{ID: "desk", LocationID: "home", Name: "Desk"}},
		Devices: []Device{{
			GroupID:    "desk",
			Serial:     "single",
			Name:       "Desk Lamp",
			Kind:       DeviceKindSingle,
			On:         false,
			Brightness: 0.65,
			Color:      &HSLColor{H: 210, S: 0.75, L: 0.55},
			Kelvin:     2700,
			Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
		}},
	}

	device := CommandSnapshotFromDeviceSnapshot(snapshot).Devices[0]
	state := device.CurrentState
	if state == nil || state.Power == nil || *state.Power {
		t.Fatalf("power = %#v", state)
	}
	if state.Brightness == nil || *state.Brightness != 65 {
		t.Fatalf("brightness = %#v", state)
	}
	if state.Hue == nil || !nearFloat(*state.Hue, 210) || state.Saturation == nil || !nearFloat(*state.Saturation, 75) {
		t.Fatalf("color = %#v", state)
	}
	if state.Kelvin == nil || *state.Kelvin != 2700 {
		t.Fatalf("kelvin = %#v", state)
	}
}

func nearFloat(a, b float64) bool {
	return math.Abs(a-b) < 0.0001
}
