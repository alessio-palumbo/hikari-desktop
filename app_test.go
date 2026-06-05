package main

import (
	"context"
	"errors"
	"testing"

	"hikari-desktop/internal/backend"
)

func TestAppUsesTransport(t *testing.T) {
	device := backend.Device{Serial: "d073d501a2c3", Name: "Test", Kind: "single"}
	transport := &recordingTransport{
		snapshot: backend.DeviceSnapshot{Devices: []backend.Device{device}},
		device:   device,
	}
	app := NewAppWithTransport(transport)
	app.startup(context.Background())

	if !transport.startCalled {
		t.Fatal("expected Start to be called")
	}

	snapshot, err := app.GetDeviceSnapshot()
	if err != nil {
		t.Fatalf("GetDeviceSnapshot returned error: %v", err)
	}
	if !transport.snapshotCalled {
		t.Fatal("expected Snapshot to be called")
	}
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].Serial != device.Serial {
		t.Fatalf("GetDeviceSnapshot returned %#v", snapshot)
	}

	got, err := app.SetDeviceState(backend.SetDeviceStateRequest{Device: device, Preview: true})
	if err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if !transport.setCalled {
		t.Fatal("expected SetDeviceState to be called")
	}
	if !transport.lastReq.Preview {
		t.Fatal("expected preview flag to be forwarded")
	}
	if got.Serial != device.Serial {
		t.Fatalf("SetDeviceState returned %#v", got)
	}

	app.shutdown(context.Background())
	if !transport.closeCalled {
		t.Fatal("expected Close to be called")
	}
}

func TestAppReturnsTransportError(t *testing.T) {
	device := backend.Device{Serial: "d073d501a2c3", Name: "Test", Kind: "single"}
	app := NewAppWithTransport(&recordingTransport{err: errors.New("boom")})
	app.startup(context.Background())

	if _, err := app.GetDeviceSnapshot(); err == nil {
		t.Fatal("GetDeviceSnapshot returned nil error, want transport error")
	}
	if _, err := app.SetDeviceState(backend.SetDeviceStateRequest{Device: device}); err == nil {
		t.Fatal("SetDeviceState returned nil error, want transport error")
	}
}

type recordingTransport struct {
	snapshot       backend.DeviceSnapshot
	device         backend.Device
	err            error
	startCalled    bool
	closeCalled    bool
	snapshotCalled bool
	setCalled      bool
	lastReq        backend.SetDeviceStateRequest
}

func (t *recordingTransport) Start(ctx context.Context) error {
	t.startCalled = true
	return t.err
}

func (t *recordingTransport) Close(ctx context.Context) error {
	t.closeCalled = true
	return t.err
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
