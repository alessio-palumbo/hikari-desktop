package main

import (
	"context"
	"errors"
	"testing"

	"hikari-desktop/internal/backend"
)

func TestAppUsesTransport(t *testing.T) {
	device := backend.Device{Serial: "d073d501a2c3", Name: "Test", Kind: backend.DeviceKindSingle}
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

	got, err := app.SetDeviceState(backend.SetDeviceStateRequest{Device: device, Preview: true, Intent: backend.DeviceCommandPower})
	if err != nil {
		t.Fatalf("SetDeviceState returned error: %v", err)
	}
	if !transport.setCalled {
		t.Fatal("expected SetDeviceState to be called")
	}
	if !transport.lastReq.Preview {
		t.Fatal("expected preview flag to be forwarded")
	}
	if transport.lastReq.Intent != "power" {
		t.Fatalf("expected power intent to be forwarded, got %q", transport.lastReq.Intent)
	}
	if got.Serial != device.Serial {
		t.Fatalf("SetDeviceState returned %#v", got)
	}

	metadataReq := backend.SetDeviceMetadataRequest{Serial: device.Serial, Label: "Renamed", LocationID: "home", GroupID: "living"}
	metadataDevice, err := app.SetDeviceMetadata(metadataReq)
	if err != nil {
		t.Fatalf("SetDeviceMetadata returned error: %v", err)
	}
	if !transport.setMetadataCalled || transport.lastMetadataReq != metadataReq {
		t.Fatalf("SetDeviceMetadata did not forward request: %#v", transport.lastMetadataReq)
	}
	if metadataDevice.Serial != device.Serial {
		t.Fatalf("SetDeviceMetadata returned %#v", metadataDevice)
	}

	startStatus, err := app.StartDeviceEffect(backend.StartDeviceEffectRequest{Device: device, Effect: backend.DeviceEffectFlame})
	if err != nil {
		t.Fatalf("StartDeviceEffect returned error: %v", err)
	}
	if !transport.startEffectCalled {
		t.Fatal("expected StartDeviceEffect to be called")
	}
	if transport.lastStartEffectReq.Effect != backend.DeviceEffectFlame {
		t.Fatalf("expected start effect request to be forwarded, got %#v", transport.lastStartEffectReq)
	}
	if startStatus.Serial != device.Serial || !startStatus.Running {
		t.Fatalf("StartDeviceEffect returned %#v", startStatus)
	}

	status, err := app.StopDeviceEffect(backend.StopDeviceEffectRequest{Device: device})
	if err != nil {
		t.Fatalf("StopDeviceEffect returned error: %v", err)
	}
	if !transport.stopEffectCalled {
		t.Fatal("expected StopDeviceEffect to be called")
	}
	if transport.lastEffectReq.Device.Serial != device.Serial {
		t.Fatalf("expected stop effect request to be forwarded, got %#v", transport.lastEffectReq)
	}
	if status.Serial != device.Serial || status.Running {
		t.Fatalf("StopDeviceEffect returned %#v", status)
	}

	settings, err := app.NetworkSettings()
	if err != nil {
		t.Fatalf("NetworkSettings returned error: %v", err)
	}
	if !transport.settingsCalled {
		t.Fatal("expected NetworkSettings to be called")
	}
	if settings.SelectedInterfaceName != "" {
		t.Fatalf("NetworkSettings returned %#v", settings)
	}

	restartSettings, err := app.RestartDeviceDiscovery()
	if err != nil {
		t.Fatalf("RestartDeviceDiscovery returned error: %v", err)
	}
	if !transport.restartCalled {
		t.Fatal("expected RestartDeviceDiscovery to be called")
	}
	if restartSettings.SelectedInterfaceName != "" {
		t.Fatalf("RestartDeviceDiscovery returned %#v", restartSettings)
	}

	networkSettings, err := app.SetNetworkInterface(backend.SetNetworkInterfaceRequest{InterfaceName: "en0"})
	if err != nil {
		t.Fatalf("SetNetworkInterface returned error: %v", err)
	}
	if !transport.setNetworkCalled {
		t.Fatal("expected SetNetworkInterface to be called")
	}
	if transport.lastNetworkReq.InterfaceName != "en0" || networkSettings.SelectedInterfaceName != "en0" {
		t.Fatalf("expected network request to be forwarded, req=%#v settings=%#v", transport.lastNetworkReq, networkSettings)
	}

	app.shutdown(context.Background())
	if !transport.closeCalled {
		t.Fatal("expected Close to be called")
	}
}

func TestAppUsesSensorProvider(t *testing.T) {
	transport := &recordingTransport{}
	sensors := &recordingSensorProvider{snapshot: backend.SensorSnapshot{Nodes: []backend.SensorNode{{ID: "sensaa-1", Name: "Bedroom"}}}}
	app := newAppWithServices(transport, sensors)
	var eventName string
	var eventSnapshot backend.SensorSnapshot
	app.emitEvent = func(_ context.Context, name string, data ...interface{}) {
		eventName = name
		if len(data) == 1 {
			eventSnapshot, _ = data[0].(backend.SensorSnapshot)
		}
	}
	app.startup(context.Background())

	if !sensors.startCalled {
		t.Fatal("expected sensor provider Start to be called")
	}
	snapshot, err := app.GetSensorSnapshot()
	if err != nil {
		t.Fatalf("GetSensorSnapshot returned error: %v", err)
	}
	if len(snapshot.Nodes) != 1 || snapshot.Nodes[0].ID != "sensaa-1" {
		t.Fatalf("GetSensorSnapshot returned %#v", snapshot)
	}
	if sensors.observer == nil {
		t.Fatal("expected sensor snapshot observer to be registered")
	}
	sensors.observer(snapshot)
	if eventName != sensorSnapshotEvent || len(eventSnapshot.Nodes) != 1 || eventSnapshot.Nodes[0].ID != "sensaa-1" {
		t.Fatalf("sensor event = %q %#v", eventName, eventSnapshot)
	}
	app.shutdown(context.Background())
	if !sensors.closeCalled {
		t.Fatal("expected sensor provider Close to be called")
	}
	if sensors.observer != nil {
		t.Fatal("expected sensor snapshot observer to be cleared")
	}
}

func TestAppUsesFloorPlanStore(t *testing.T) {
	store := &recordingFloorPlanStore{document: backend.FloorPlanPreferencesDocument{
		Exists: true,
		Data:   `{"version":2,"profiles":{}}`,
	}}
	app := NewAppWithTransport(&recordingTransport{})
	app.floorPlans = store

	document, err := app.GetFloorPlanPreferences()
	if err != nil {
		t.Fatalf("GetFloorPlanPreferences returned error: %v", err)
	}
	if !store.loadCalled || !document.Exists || document.Data != store.document.Data {
		t.Fatalf("GetFloorPlanPreferences returned %#v", document)
	}
	if err := app.SaveFloorPlanPreferences(backend.SaveFloorPlanPreferencesRequest{Data: document.Data}); err != nil {
		t.Fatalf("SaveFloorPlanPreferences returned error: %v", err)
	}
	if !store.saveCalled || store.saved != document.Data {
		t.Fatalf("SaveFloorPlanPreferences saved %q", store.saved)
	}
}

func TestAppReturnsTransportError(t *testing.T) {
	device := backend.Device{Serial: "d073d501a2c3", Name: "Test", Kind: backend.DeviceKindSingle}
	app := NewAppWithTransport(&recordingTransport{err: errors.New("boom")})
	app.startup(context.Background())

	if _, err := app.GetDeviceSnapshot(); err == nil {
		t.Fatal("GetDeviceSnapshot returned nil error, want transport error")
	}
	if _, err := app.SetDeviceState(backend.SetDeviceStateRequest{Device: device}); err == nil {
		t.Fatal("SetDeviceState returned nil error, want transport error")
	}
	if _, err := app.SetDeviceMetadata(backend.SetDeviceMetadataRequest{Serial: device.Serial}); err == nil {
		t.Fatal("SetDeviceMetadata returned nil error, want transport error")
	}
	if _, err := app.NetworkSettings(); err == nil {
		t.Fatal("NetworkSettings returned nil error, want transport error")
	}
	if _, err := app.SetNetworkInterface(backend.SetNetworkInterfaceRequest{InterfaceName: "en0"}); err == nil {
		t.Fatal("SetNetworkInterface returned nil error, want transport error")
	}
	if _, err := app.RestartDeviceDiscovery(); err == nil {
		t.Fatal("RestartDeviceDiscovery returned nil error, want transport error")
	}
	if _, err := app.StartDeviceEffect(backend.StartDeviceEffectRequest{Device: device}); err == nil {
		t.Fatal("StartDeviceEffect returned nil error, want transport error")
	}
	if _, err := app.StopDeviceEffect(backend.StopDeviceEffectRequest{Device: device}); err == nil {
		t.Fatal("StopDeviceEffect returned nil error, want transport error")
	}
}

type recordingTransport struct {
	snapshot           backend.DeviceSnapshot
	device             backend.Device
	err                error
	startCalled        bool
	closeCalled        bool
	snapshotCalled     bool
	setCalled          bool
	setMetadataCalled  bool
	startEffectCalled  bool
	stopEffectCalled   bool
	settingsCalled     bool
	setNetworkCalled   bool
	restartCalled      bool
	lastReq            backend.SetDeviceStateRequest
	lastMetadataReq    backend.SetDeviceMetadataRequest
	lastStartEffectReq backend.StartDeviceEffectRequest
	lastEffectReq      backend.StopDeviceEffectRequest
	lastNetworkReq     backend.SetNetworkInterfaceRequest
}

type recordingSensorProvider struct {
	snapshot    backend.SensorSnapshot
	startCalled bool
	closeCalled bool
	observer    func(backend.SensorSnapshot)
}

func (s *recordingSensorProvider) SetSnapshotObserver(observer func(backend.SensorSnapshot)) {
	s.observer = observer
}

type recordingFloorPlanStore struct {
	document   backend.FloorPlanPreferencesDocument
	saved      string
	loadCalled bool
	saveCalled bool
}

func (s *recordingFloorPlanStore) Load() (backend.FloorPlanPreferencesDocument, error) {
	s.loadCalled = true
	return s.document, nil
}

func (s *recordingFloorPlanStore) Save(data string) error {
	s.saveCalled = true
	s.saved = data
	return nil
}

func (s *recordingSensorProvider) Start(context.Context) error {
	s.startCalled = true
	return nil
}

func (s *recordingSensorProvider) Close(context.Context) error {
	s.closeCalled = true
	return nil
}

func (s *recordingSensorProvider) Snapshot(context.Context) (backend.SensorSnapshot, error) {
	return s.snapshot, nil
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

func (t *recordingTransport) NetworkSettings(ctx context.Context) (backend.NetworkSettings, error) {
	t.settingsCalled = true
	return backend.NetworkSettings{SelectedInterfaceName: "", Interfaces: []backend.NetworkInterface{}}, t.err
}

func (t *recordingTransport) SetNetworkInterface(ctx context.Context, req backend.SetNetworkInterfaceRequest) (backend.NetworkSettings, error) {
	t.setNetworkCalled = true
	t.lastNetworkReq = req
	return backend.NetworkSettings{SelectedInterfaceName: req.InterfaceName, Interfaces: []backend.NetworkInterface{}}, t.err
}

func (t *recordingTransport) RestartDeviceDiscovery(ctx context.Context) (backend.NetworkSettings, error) {
	t.restartCalled = true
	return backend.NetworkSettings{SelectedInterfaceName: "", Interfaces: []backend.NetworkInterface{}}, t.err
}

func (t *recordingTransport) SetDeviceState(ctx context.Context, req backend.SetDeviceStateRequest) (backend.Device, error) {
	t.setCalled = true
	t.lastReq = req
	return t.device, t.err
}

func (t *recordingTransport) SetDeviceMetadata(ctx context.Context, req backend.SetDeviceMetadataRequest) (backend.Device, error) {
	t.setMetadataCalled = true
	t.lastMetadataReq = req
	return t.device, t.err
}

func (t *recordingTransport) StartDeviceEffect(ctx context.Context, req backend.StartDeviceEffectRequest) (backend.DeviceEffectStatus, error) {
	t.startEffectCalled = true
	t.lastStartEffectReq = req
	return backend.DeviceEffectStatus{Serial: req.Device.Serial, Running: true, Effect: string(req.Effect)}, t.err
}

func (t *recordingTransport) StopDeviceEffect(ctx context.Context, req backend.StopDeviceEffectRequest) (backend.DeviceEffectStatus, error) {
	t.stopEffectCalled = true
	t.lastEffectReq = req
	return backend.DeviceEffectStatus{Serial: req.Device.Serial, Running: false}, t.err
}
