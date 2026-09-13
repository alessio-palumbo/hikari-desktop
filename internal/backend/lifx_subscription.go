package backend

import (
	"context"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

// startDeviceSubscription follows one controller for its complete lifetime.
// A generation prevents events from a replaced controller from entering the
// inventory selected for a newer network interface.
func (t *LifxTransport) startDeviceSubscription(ctrl lifxController) {
	ctx, cancel := context.WithCancel(context.Background())
	events := ctrl.SubscribeDevices(ctx)
	done := make(chan struct{})

	t.mu.Lock()
	if t.controller != ctrl || t.subscriptionCancel != nil {
		t.mu.Unlock()
		cancel()
		return
	}
	t.subscriptionGeneration++
	generation := t.subscriptionGeneration
	t.subscriptionCancel = cancel
	t.subscriptionDone = done
	t.observed = make(map[lifxdevice.Serial]lifxdevice.Device)
	t.observedReady = false
	t.observedRevision = 0
	t.mu.Unlock()

	go t.consumeDeviceEvents(ctrl, generation, events, done)
}

func (t *LifxTransport) stopDeviceSubscription() {
	t.mu.Lock()
	cancel := t.subscriptionCancel
	done := t.subscriptionDone
	t.subscriptionCancel = nil
	t.subscriptionDone = nil
	t.subscriptionGeneration++
	t.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (t *LifxTransport) consumeDeviceEvents(ctrl lifxController, generation uint64, events <-chan lifxcontroller.DeviceEvent, done chan<- struct{}) {
	defer close(done)
	for event := range events {
		switch event.Type {
		case lifxcontroller.DeviceEventAdded, lifxcontroller.DeviceEventUpdated:
			t.storeObservedDevice(generation, event)
		case lifxcontroller.DeviceEventRemoved:
			t.removeObservedDevice(generation, event)
		case lifxcontroller.DeviceEventSnapshotComplete:
			t.completeObservedSnapshot(generation, event.Revision)
		case lifxcontroller.DeviceEventResyncRequired:
			t.resyncObservedDevices(ctrl, generation, event.Revision)
		}
	}
}

func (t *LifxTransport) storeObservedDevice(generation uint64, event lifxcontroller.DeviceEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if generation != t.subscriptionGeneration || (t.observedReady && event.Revision <= t.observedRevision) {
		return
	}
	t.observed[event.Device.Serial] = event.Device.Clone()
	if !event.Initial {
		t.observedRevision = event.Revision
	}
}

func (t *LifxTransport) removeObservedDevice(generation uint64, event lifxcontroller.DeviceEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if generation != t.subscriptionGeneration || !t.observedReady || event.Revision <= t.observedRevision {
		return
	}
	delete(t.observed, event.Device.Serial)
	t.observedRevision = event.Revision
}

func (t *LifxTransport) completeObservedSnapshot(generation uint64, revision uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if generation != t.subscriptionGeneration || t.observedReady {
		return
	}
	t.observedReady = true
	t.observedRevision = revision
}

func (t *LifxTransport) resyncObservedDevices(ctrl lifxController, generation uint64, revision uint64) {
	devices := ctrl.GetDevices()
	next := make(map[lifxdevice.Serial]lifxdevice.Device, len(devices))
	for _, device := range devices {
		next[device.Serial] = device.Clone()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if generation != t.subscriptionGeneration || revision < t.observedRevision {
		return
	}
	t.observed = next
	t.observedReady = true
	t.observedRevision = revision
}

func (t *LifxTransport) snapshotSourceDevices(ctrl lifxController) []lifxdevice.Device {
	t.mu.RLock()
	if !t.observedReady {
		t.mu.RUnlock()
		return ctrl.GetDevices()
	}
	devices := make([]lifxdevice.Device, 0, len(t.observed))
	for _, device := range t.observed {
		devices = append(devices, device.Clone())
	}
	t.mu.RUnlock()
	lifxdevice.SortDevices(devices)
	return devices
}
