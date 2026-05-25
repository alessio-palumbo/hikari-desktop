package backend

import (
	"context"
	"testing"
)

func TestMockTransportSnapshot(t *testing.T) {
	transport := NewMockTransport()

	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if len(snapshot.Locations) == 0 {
		t.Fatal("expected mock locations")
	}
	if len(snapshot.Groups) == 0 {
		t.Fatal("expected mock groups")
	}
	if len(snapshot.Devices) == 0 {
		t.Fatal("expected mock devices")
	}
}

func TestMockTransportSetDeviceState(t *testing.T) {
	transport := NewMockTransport()
	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	device := snapshot.Devices[0]
	device.On = !device.On

	got, err := transport.SetDeviceState(context.Background(), SetDeviceStateRequest{Device: device})
	if err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if got.ID != device.ID || got.On != device.On {
		t.Fatalf("SetDeviceState returned %#v, want %#v", got, device)
	}
}
