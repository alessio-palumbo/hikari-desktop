package main

import (
	"context"
	"errors"
	"testing"

	"hikari-desktop/internal/backend"
)

func TestAppUsesTransport(t *testing.T) {
	device := backend.Device{ID: "test", Name: "Test", Kind: "single"}
	transport := &recordingTransport{
		snapshot: backend.DeviceSnapshot{Devices: []backend.Device{device}},
		device:   device,
	}
	app := NewAppWithTransport(transport)
	app.startup(context.Background())

	snapshot := app.GetDeviceSnapshot()
	if !transport.snapshotCalled {
		t.Fatal("expected Snapshot to be called")
	}
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].ID != device.ID {
		t.Fatalf("GetDeviceSnapshot returned %#v", snapshot)
	}

	got := app.SetDeviceState(backend.SetDeviceStateRequest{Device: device, Preview: true})
	if !transport.setCalled {
		t.Fatal("expected SetDeviceState to be called")
	}
	if !transport.lastReq.Preview {
		t.Fatal("expected preview flag to be forwarded")
	}
	if got.ID != device.ID {
		t.Fatalf("SetDeviceState returned %#v", got)
	}
}

func TestAppFallbacksOnTransportError(t *testing.T) {
	device := backend.Device{ID: "test", Name: "Test", Kind: "single"}
	app := NewAppWithTransport(&recordingTransport{err: errors.New("boom")})
	app.startup(context.Background())

	if got := app.GetDeviceSnapshot(); len(got.Devices) != 0 {
		t.Fatalf("GetDeviceSnapshot returned %#v, want empty snapshot", got)
	}
	if got := app.SetDeviceState(backend.SetDeviceStateRequest{Device: device}); got.ID != device.ID {
		t.Fatalf("SetDeviceState returned %#v, want request device", got)
	}
}

type recordingTransport struct {
	snapshot       backend.DeviceSnapshot
	device         backend.Device
	err            error
	snapshotCalled bool
	setCalled      bool
	lastReq        backend.SetDeviceStateRequest
}

func (t *recordingTransport) Snapshot(ctx context.Context) (backend.DeviceSnapshot, error) {
	t.snapshotCalled = true
	return t.snapshot, t.err
}

func (t *recordingTransport) SetDeviceState(ctx context.Context, req backend.SetDeviceStateRequest) (backend.Device, error) {
	t.setCalled = true
	t.lastReq = req
	return t.device, t.err
}
