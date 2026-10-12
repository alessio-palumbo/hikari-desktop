package backend

import (
	"context"
	"errors"
	"time"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"
)

type OnboardingNetwork interface {
	CheckSetupReady() error
	SetupVerifier(context.Context) SetupVerifier
	RefreshAfterSetup(context.Context) error
}

func (t *LifxTransport) CheckSetupReady() error {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(t.effects) > 0 {
		return errors.New("stop running hikari effects before setting up a device")
	}
	return nil
}

func (t *LifxTransport) RefreshAfterSetup(ctx context.Context) error {
	_, err := t.RestartDeviceDiscovery(ctx)
	return err
}

func (t *LifxTransport) SetupVerifier(ctx context.Context) SetupVerifier {
	return &setupDeviceVerifier{transport: t, ctx: ctx}
}

type setupDeviceVerifier struct {
	transport *LifxTransport
	ctx       context.Context
	refreshed bool
	since     time.Time
}

func (v *setupDeviceVerifier) GetDevices() []lifxdevice.Device {
	if v.ctx.Err() != nil {
		return nil
	}
	if !v.refreshed {
		v.since = time.Now()
		// First verification read happens after the library rejoins Wi-Fi. This
		// refreshes endpoints on the existing controller; only a closed socket or
		// an intentionally changed interface causes controller recreation.
		if err := v.transport.RefreshAfterSetup(v.ctx); err != nil {
			return nil
		}
		v.refreshed = true
	}
	ctrl := v.transport.currentController()
	if ctrl == nil {
		return nil
	}
	var result []lifxdevice.Device
	for _, d := range ctrl.GetDevices() {
		if d.LastSeenAt.After(v.since) {
			result = append(result, d)
		}
	}
	return result
}
