package backend

import "testing"

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
			Capability: DeviceCapability{HasColor: true, KelvinMin: 1500, KelvinMax: 9000},
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
