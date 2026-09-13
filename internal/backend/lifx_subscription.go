package backend

import (
	"context"
	"time"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

// deviceSnapshotNotificationDelay coalesces bursts of per-device controller
// events into one UI snapshot. The inventory is updated immediately for every
// event; only the comparatively expensive Wails/frontend notification waits.
const deviceSnapshotNotificationDelay = 75 * time.Millisecond

// SetSnapshotObserver registers the delivery boundary for revisioned device
// snapshots. The observer should return quickly; UI delivery is coalesced but
// device observation itself is not throttled.
func (t *LifxTransport) SetSnapshotObserver(observer func(DeviceSnapshot)) {
	t.mu.Lock()
	t.snapshotObserver = observer
	t.mu.Unlock()
}

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
	timer := time.NewTimer(deviceSnapshotNotificationDelay)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	var timerC <-chan time.Time
	dirty := false

	for {
		select {
		case event, ok := <-events:
			if !ok {
				if dirty {
					t.notifyDeviceSnapshot(ctrl, generation)
				}
				return
			}
			changed, ready := t.applyDeviceEvent(ctrl, generation, event)
			if changed && ready {
				dirty = true
				if timerC == nil {
					timer.Reset(deviceSnapshotNotificationDelay)
					timerC = timer.C
				}
			}
		case <-timerC:
			timerC = nil
			if dirty {
				t.notifyDeviceSnapshot(ctrl, generation)
				dirty = false
			}
		}
	}
}

func (t *LifxTransport) applyDeviceEvent(ctrl lifxController, generation uint64, event lifxcontroller.DeviceEvent) (bool, bool) {
	switch event.Type {
	case lifxcontroller.DeviceEventAdded, lifxcontroller.DeviceEventUpdated:
		return t.storeObservedDevice(generation, event)
	case lifxcontroller.DeviceEventRemoved:
		return t.removeObservedDevice(generation, event)
	case lifxcontroller.DeviceEventSnapshotComplete:
		return t.completeObservedSnapshot(generation, event.Revision)
	case lifxcontroller.DeviceEventResyncRequired:
		return t.resyncObservedDevices(ctrl, generation, event.Revision)
	default:
		return false, false
	}
}

func (t *LifxTransport) storeObservedDevice(generation uint64, event lifxcontroller.DeviceEvent) (bool, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if generation != t.subscriptionGeneration || (t.observedReady && event.Revision <= t.observedRevision) {
		return false, t.observedReady
	}
	t.observed[event.Device.Serial] = event.Device.Clone()
	if !event.Initial {
		t.observedRevision = event.Revision
	}
	t.snapshotRevision++
	return true, t.observedReady
}

func (t *LifxTransport) removeObservedDevice(generation uint64, event lifxcontroller.DeviceEvent) (bool, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if generation != t.subscriptionGeneration || !t.observedReady || event.Revision <= t.observedRevision {
		return false, t.observedReady
	}
	delete(t.observed, event.Device.Serial)
	t.observedRevision = event.Revision
	t.snapshotRevision++
	return true, true
}

func (t *LifxTransport) completeObservedSnapshot(generation uint64, revision uint64) (bool, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if generation != t.subscriptionGeneration || t.observedReady {
		return false, t.observedReady
	}
	t.observedReady = true
	t.observedRevision = revision
	t.snapshotRevision++
	return true, true
}

func (t *LifxTransport) resyncObservedDevices(ctrl lifxController, generation uint64, revision uint64) (bool, bool) {
	devices := ctrl.GetDevices()
	next := make(map[lifxdevice.Serial]lifxdevice.Device, len(devices))
	for _, device := range devices {
		next[device.Serial] = device.Clone()
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if generation != t.subscriptionGeneration || revision < t.observedRevision {
		return false, t.observedReady
	}
	t.observed = next
	t.observedReady = true
	t.observedRevision = revision
	t.snapshotRevision++
	return true, true
}

func (t *LifxTransport) snapshotSourceDevices(ctrl lifxController) ([]lifxdevice.Device, uint64) {
	t.mu.RLock()
	if !t.observedReady {
		revision := t.snapshotRevision
		t.mu.RUnlock()
		return ctrl.GetDevices(), revision
	}
	devices := make([]lifxdevice.Device, 0, len(t.observed))
	for _, device := range t.observed {
		devices = append(devices, device.Clone())
	}
	revision := t.snapshotRevision
	t.mu.RUnlock()
	lifxdevice.SortDevices(devices)
	return devices, revision
}

func (t *LifxTransport) notifyDeviceSnapshot(ctrl lifxController, generation uint64) {
	t.mu.RLock()
	if generation != t.subscriptionGeneration || !t.observedReady || t.snapshotObserver == nil {
		t.mu.RUnlock()
		return
	}
	observer := t.snapshotObserver
	t.mu.RUnlock()

	devices, revision := t.snapshotSourceDevices(ctrl)
	snapshot := mapLifxDevices(devices)
	snapshot.Revision = revision
	t.reconcileFirmwareEffectSnapshot(&snapshot, time.Now())
	t.reconcileRestoreSnapshot(&snapshot, time.Now())
	t.reconcileMetadataSnapshot(&snapshot, time.Now())
	snapshot = sortDeviceSnapshot(snapshot)
	t.replaceCache(snapshot.Devices)
	observer(snapshot)
}
