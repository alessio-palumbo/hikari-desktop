package backend

import (
	"context"
	"fmt"

	lifxcontroller "github.com/alessio-palumbo/lifxlan-go/pkg/controller"
	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

func deviceEndpointChanged(previous, next lifxdevice.Device) bool {
	return previous.Address != nil && next.Address != nil && previous.Address.String() != next.Address.String()
}

func (t *LifxTransport) validateEndpointRequest(device Device) error {
	t.mu.RLock()
	revision := t.endpointRevision[device.Serial]
	t.mu.RUnlock()
	if revision != device.EndpointRevision {
		return fmt.Errorf("device address changed; refresh its state before trying again: %w", lifxcontroller.ErrEndpointChanged)
	}
	return nil
}

// Caller holds t.mu. Never restore an old endpoint's state or keep an animation
// rendering into a newly discovered endpoint before its observations are ready.
func (t *LifxTransport) invalidateEndpointLocked(serial string) {
	t.endpointRevision[serial]++
	t.endpointNeedsState[serial] = true
	delete(t.cache, serial)
	delete(t.metadata, serial)
	delete(t.restores, serial)
	delete(t.firmware, serial)
	if effect, ok := t.effects[serial]; ok {
		effect.cancel()
		delete(t.effects, serial)
	}
}

// An endpoint move invalidates observations, not the stable device identity.
// Dependent writes must reacquire state; a failed write is never retried here.
func (t *LifxTransport) refreshEndpointState(ctx context.Context, ctrl lifxController, serial lifxdevice.Serial) error {
	t.mu.RLock()
	needed := t.endpointNeedsState[serial.String()]
	revision := t.endpointRevision[serial.String()]
	t.mu.RUnlock()
	if !needed {
		return nil
	}
	if _, err := ctrl.CaptureStateSnapshot(ctx, []lifxdevice.Serial{serial}, lifxcontroller.SnapshotOptions{RequireFresh: true}); err != nil {
		return fmt.Errorf("observe device after address change: %w", err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.controller != ctrl || t.endpointRevision[serial.String()] != revision {
		return lifxcontroller.ErrEndpointChanged
	}
	delete(t.endpointNeedsState, serial.String())
	return nil
}
