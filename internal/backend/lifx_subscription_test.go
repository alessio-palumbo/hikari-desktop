package backend

import (
	"context"
	"testing"
	"time"

	lifxclient "github.com/alessio-palumbo/lifxlan-go/pkg/client"
	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

func TestLifxTransportSnapshotUsesSubscribedInventory(t *testing.T) {
	initial := testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")
	events := make(chan lifxcontroller.DeviceEvent, 4)
	controller := &fakeLifxController{devices: []lifxdevice.Device{initial}, events: events}
	transport := newTestLifxTransport(t, controller)
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	waitForObservedRevision(t, transport, 0, true)

	controller.mu.Lock()
	before := controller.getDevicesCalls
	controller.mu.Unlock()
	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	controller.mu.Lock()
	after := controller.getDevicesCalls
	controller.mu.Unlock()
	if after != before {
		t.Fatalf("GetDevices calls = %d -> %d, want subscription-backed snapshot", before, after)
	}
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].Name != initial.Label {
		t.Fatalf("snapshot = %#v", snapshot)
	}

	updated := initial.Clone()
	updated.Label = "Updated Lamp"
	events <- lifxcontroller.DeviceEvent{Type: lifxcontroller.DeviceEventUpdated, Device: updated, Revision: 1, Changes: lifxcontroller.DeviceChangeLabel}
	waitForObservedRevision(t, transport, 1, true)
	snapshot, err = transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("updated Snapshot returned error: %v", err)
	}
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].Name != updated.Label {
		t.Fatalf("updated snapshot = %#v", snapshot)
	}
}

func TestLifxTransportSubscriptionRemovesDevice(t *testing.T) {
	device := testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")
	events := make(chan lifxcontroller.DeviceEvent, 2)
	controller := &fakeLifxController{devices: []lifxdevice.Device{device}, events: events}
	transport := newTestLifxTransport(t, controller)
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	waitForObservedRevision(t, transport, 0, true)

	events <- lifxcontroller.DeviceEvent{Type: lifxcontroller.DeviceEventRemoved, Device: device, Revision: 1}
	waitForObservedRevision(t, transport, 1, true)
	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if len(snapshot.Devices) != 0 {
		t.Fatalf("snapshot devices = %#v, want removed", snapshot.Devices)
	}
}

func TestLifxTransportSubscriptionResyncsCompleteInventory(t *testing.T) {
	first := testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")
	second := testLifxDevice(t, "d073d501a2c4", "Pendant", "Home", "Kitchen")
	events := make(chan lifxcontroller.DeviceEvent, 2)
	controller := &fakeLifxController{devices: []lifxdevice.Device{first}, events: events}
	transport := newTestLifxTransport(t, controller)
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	waitForObservedRevision(t, transport, 0, true)

	controller.setDevices([]lifxdevice.Device{second})
	events <- lifxcontroller.DeviceEvent{Type: lifxcontroller.DeviceEventResyncRequired, Revision: 4}
	waitForObservedRevision(t, transport, 4, true)
	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].Serial != second.Serial.String() {
		t.Fatalf("snapshot = %#v, want resynced device %s", snapshot, second.Serial)
	}
	controller.mu.Lock()
	getDevicesCalls := controller.getDevicesCalls
	controller.mu.Unlock()
	if getDevicesCalls != 1 {
		t.Fatalf("GetDevices calls = %d, want one resync", getDevicesCalls)
	}
}

func TestLifxTransportSubscriptionCoalescesSnapshotNotifications(t *testing.T) {
	initial := testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")
	events := make(chan lifxcontroller.DeviceEvent, 4)
	controller := &fakeLifxController{devices: []lifxdevice.Device{initial}, events: events}
	transport := newTestLifxTransport(t, controller)
	notifications := make(chan DeviceSnapshot, 4)
	transport.SetSnapshotObserver(func(snapshot DeviceSnapshot) { notifications <- snapshot })
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	first := waitForDeviceSnapshot(t, notifications)

	updated := initial.Clone()
	updated.Label = "First update"
	events <- lifxcontroller.DeviceEvent{Type: lifxcontroller.DeviceEventUpdated, Device: updated, Revision: 1, Changes: lifxcontroller.DeviceChangeLabel}
	updated.Label = "Latest update"
	events <- lifxcontroller.DeviceEvent{Type: lifxcontroller.DeviceEventUpdated, Device: updated, Revision: 2, Changes: lifxcontroller.DeviceChangeLabel}

	latest := waitForDeviceSnapshot(t, notifications)
	if latest.Revision <= first.Revision {
		t.Fatalf("revision = %d after %d, want monotonic increase", latest.Revision, first.Revision)
	}
	if len(latest.Devices) != 1 || latest.Devices[0].Name != "Latest update" {
		t.Fatalf("notification = %#v, want latest coalesced state", latest)
	}
	select {
	case extra := <-notifications:
		t.Fatalf("unexpected uncoalesced notification: %#v", extra)
	case <-time.After(2 * deviceSnapshotNotificationDelay):
	}
}

func TestLifxTransportRestartReplacesDeviceSubscription(t *testing.T) {
	firstEvents := make(chan lifxcontroller.DeviceEvent, 2)
	secondEvents := make(chan lifxcontroller.DeviceEvent, 2)
	first := &fakeLifxController{
		devices: []lifxdevice.Device{testLifxDevice(t, "d073d501a2c3", "Desk Lamp", "Home", "Desk")},
		events:  firstEvents,
	}
	second := &fakeLifxController{
		devices: []lifxdevice.Device{testLifxDevice(t, "d073d501a2c4", "Pendant", "Office", "Kitchen")},
		events:  secondEvents,
	}
	controllers := []lifxController{first, second}
	transport := newLifxTransport(func(*lifxclient.Config) (lifxController, error) {
		controller := controllers[0]
		controllers = controllers[1:]
		return controller, nil
	}, func() ([]lifxclient.BroadcastInterface, error) {
		return []lifxclient.BroadcastInterface{testBroadcastInterface("en0", "192.168.1.42", "192.168.1.255")}, nil
	}, &memoryNetworkSettingsStore{})
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start returned error: %v", err)
	}
	t.Cleanup(func() { _ = transport.Close(context.Background()) })
	waitForObservedRevision(t, transport, 0, true)

	if _, err := transport.SetNetworkInterface(context.Background(), SetNetworkInterfaceRequest{InterfaceName: "en0"}); err != nil {
		t.Fatalf("SetNetworkInterface returned error: %v", err)
	}
	waitForObservedRevision(t, transport, 0, true)

	stale := first.devices[0].Clone()
	stale.Label = "Stale"
	firstEvents <- lifxcontroller.DeviceEvent{Type: lifxcontroller.DeviceEventUpdated, Device: stale, Revision: 99}
	snapshot, err := transport.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot returned error: %v", err)
	}
	if len(snapshot.Devices) != 1 || snapshot.Devices[0].Serial != second.devices[0].Serial.String() {
		t.Fatalf("snapshot = %#v, want replacement controller inventory", snapshot)
	}
	if first.subscribeCalls != 1 || second.subscribeCalls != 1 {
		t.Fatalf("subscribe calls = %d/%d, want one per controller", first.subscribeCalls, second.subscribeCalls)
	}
}

func waitForObservedRevision(t *testing.T, transport *LifxTransport, revision uint64, ready bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		transport.mu.RLock()
		gotRevision := transport.observedRevision
		gotReady := transport.observedReady
		transport.mu.RUnlock()
		if gotRevision == revision && gotReady == ready {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("observed inventory did not reach revision %d ready=%v", revision, ready)
}

func waitForDeviceSnapshot(t *testing.T, snapshots <-chan DeviceSnapshot) DeviceSnapshot {
	t.Helper()
	select {
	case snapshot := <-snapshots:
		return snapshot
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for device snapshot")
		return DeviceSnapshot{}
	}
}
