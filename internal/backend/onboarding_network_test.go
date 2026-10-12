package backend

import (
	"context"
	"testing"
	"time"
)

func TestSetupVerifierRefreshesExistingControllerAndRejectsStaleInventory(t *testing.T) {
	ctrl, _ := newPartTestController(t)
	ctrl.devices[0].LastSeenAt = time.Unix(1, 0)
	transport := newTestLifxTransport(t, ctrl)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	verifier := transport.SetupVerifier(ctx).(*setupDeviceVerifier)
	if got := verifier.GetDevices(); len(got) != 0 {
		t.Fatal("pre-setup device treated as verified")
	}
	if ctrl.refreshes != 1 || ctrl.closed || transport.currentController() != ctrl {
		t.Fatal("controller replaced during same-interface handoff")
	}
	ctrl.devices[0].LastSeenAt = verifier.since.Add(time.Second)
	if got := verifier.GetDevices(); len(got) != 1 {
		t.Fatal("fresh device not exposed to library verification")
	}
	if ctrl.refreshes != 1 {
		t.Fatal("verification repeated discovery refresh")
	}
	cancel()
	if got := verifier.GetDevices(); len(got) != 0 {
		t.Fatal("cancelled verification read inventory")
	}
}

func TestSetupRejectsRunningHikariEffects(t *testing.T) {
	transport := NewLifxTransportWithController(nil)
	transport.effects["test"] = runningAppEffect{}
	if err := transport.CheckSetupReady(); err == nil {
		t.Fatal("running LAN animation allowed through Wi-Fi handoff")
	}
	delete(transport.effects, "test")
	transport.firmware["test"] = runningFirmwareEffect{}
	if err := transport.CheckSetupReady(); err != nil {
		t.Fatal("on-device effect unnecessarily blocked setup")
	}
}
