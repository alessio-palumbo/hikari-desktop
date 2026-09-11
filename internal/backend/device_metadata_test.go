package backend

import (
	"context"
	"strings"
	"testing"
)

func TestMockTransportSetDeviceMetadataUpdatesSnapshot(t *testing.T) {
	transport := NewMockTransport()
	initial, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	device := initial.Devices[0]

	got, err := transport.SetDeviceMetadata(context.Background(), SetDeviceMetadataRequest{
		Serial:     device.Serial,
		Label:      "  Renamed light  ",
		LocationID: "studio",
		GroupID:    "desk",
	})
	if err != nil {
		t.Fatalf("SetDeviceMetadata returned error: %v", err)
	}
	if got.Name != "Renamed light" || got.GroupID != "desk" {
		t.Fatalf("device = %#v", got)
	}

	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	updated := deviceBySerial(snapshot.Devices, device.Serial)
	if updated == nil || updated.Name != "Renamed light" || updated.GroupID != "desk" {
		t.Fatalf("snapshot device = %#v", updated)
	}
}

func TestResolveDeviceMetadataRejectsGroupFromAnotherLocation(t *testing.T) {
	snapshot := MockDeviceSnapshot()
	device := snapshot.Devices[0]

	_, _, _, _, err := resolveDeviceMetadata(snapshot, SetDeviceMetadataRequest{
		Serial:     device.Serial,
		Label:      device.Name,
		LocationID: "home",
		GroupID:    "desk",
	})
	if err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("error = %v, want location mismatch", err)
	}
}

func TestResolveDeviceMetadataRejectsLabelsOver32Bytes(t *testing.T) {
	snapshot := MockDeviceSnapshot()
	device := snapshot.Devices[0]

	_, _, _, _, err := resolveDeviceMetadata(snapshot, SetDeviceMetadataRequest{
		Serial:     device.Serial,
		Label:      strings.Repeat("a", lifxLabelMaxBytes+1),
		LocationID: "home",
		GroupID:    "living",
	})
	if err == nil || !strings.Contains(err.Error(), "maximum is 32") {
		t.Fatalf("error = %v, want label length error", err)
	}
}

func deviceBySerial(devices []Device, serial string) *Device {
	for index := range devices {
		if devices[index].Serial == serial {
			return &devices[index]
		}
	}
	return nil
}
