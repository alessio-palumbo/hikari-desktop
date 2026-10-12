package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	lifxdevice "github.com/alessio-palumbo/lifxlan-go/pkg/device"

	"hikari-desktop/internal/backend"
)

type recordingOnboarding struct {
	scans, cancels, closes int
	provision              func(context.Context, backend.SetupLightRequest, backend.SetupVerifier, func(backend.SetupProgress)) (backend.SetupLightResult, error)
}

func (*recordingOnboarding) Capabilities() backend.OnboardingCapabilities {
	return backend.OnboardingCapabilities{Available: true}
}
func (s *recordingOnboarding) Discover(context.Context, bool) (backend.SetupLightDiscovery, error) {
	s.scans++
	return backend.SetupLightDiscovery{Lights: []backend.SetupLight{}}, nil
}
func (s *recordingOnboarding) CancelScan()                 { s.cancels++ }
func (s *recordingOnboarding) Close(context.Context) error { s.closes++; return nil }

func (s *recordingOnboarding) Provision(ctx context.Context, req backend.SetupLightRequest, verifier backend.SetupVerifier, progress func(backend.SetupProgress)) (backend.SetupLightResult, error) {
	return s.provision(ctx, req, verifier, progress)
}

type setupRecordingTransport struct {
	recordingTransport
	refreshes atomic.Int32
}

func (*setupRecordingTransport) CheckSetupReady() error                                { return nil }
func (s *setupRecordingTransport) SetupVerifier(context.Context) backend.SetupVerifier { return s }
func (*setupRecordingTransport) GetDevices() []lifxdevice.Device                       { return nil }
func (s *setupRecordingTransport) RefreshAfterSetup(context.Context) error {
	s.refreshes.Add(1)
	return nil
}

func TestAppOnboardingIsLazyAndClosedOnShutdown(t *testing.T) {
	app := NewAppWithTransport(&setupRecordingTransport{})
	setup := &recordingOnboarding{}
	app.onboarding = setup
	app.startup(context.Background())
	if !app.OnboardingCapabilities().Available || setup.scans != 0 {
		t.Fatal("startup scanned for setup devices")
	}
	if _, err := app.ScanSetupLights(false); err != nil || setup.scans != 1 {
		t.Fatal("explicit setup scan did not delegate")
	}
	app.CancelLightSetup("")
	app.shutdown(context.Background())
	if setup.cancels != 1 || setup.closes != 1 {
		t.Fatal("setup resources were not cancelled/closed")
	}
}

func TestSetupPausesControlsAndSnapshotsUntilWiFiRecovery(t *testing.T) {
	transport := &setupRecordingTransport{}
	app := NewAppWithTransport(transport)
	started, finish := make(chan struct{}), make(chan struct{})
	app.onboarding = &recordingOnboarding{provision: func(_ context.Context, req backend.SetupLightRequest, _ backend.SetupVerifier, emit func(backend.SetupProgress)) (backend.SetupLightResult, error) {
		emit(backend.SetupProgress{AttemptID: req.AttemptID, Phase: "join"})
		close(started)
		<-finish
		return backend.SetupLightResult{Verified: true, Serial: "d073d501a2c3"}, nil
	}}
	app.emitEvent = func(_ context.Context, name string, data ...interface{}) {
		if name != setupProgressEvent {
			t.Error("wrong setup event")
		}
	}
	done := make(chan error, 1)
	go func() { _, err := app.SetupLight(backend.SetupLightRequest{AttemptID: "one"}); done <- err }()
	<-started
	if _, err := app.GetDeviceSnapshot(); !errors.Is(err, backend.ErrOnboardingBusy) {
		t.Fatalf("snapshot=%v", err)
	}
	if _, err := app.SetDeviceState(backend.SetDeviceStateRequest{}); !errors.Is(err, backend.ErrOnboardingBusy) {
		t.Fatalf("control=%v", err)
	}
	if _, err := app.RestartDeviceDiscovery(); !errors.Is(err, backend.ErrOnboardingBusy) {
		t.Fatalf("restart=%v", err)
	}
	if _, err := app.SetNetworkInterface(backend.SetNetworkInterfaceRequest{}); !errors.Is(err, backend.ErrOnboardingBusy) {
		t.Fatalf("interface change=%v", err)
	}
	if _, err := app.SetupLight(backend.SetupLightRequest{}); !errors.Is(err, backend.ErrOnboardingBusy) {
		t.Fatalf("duplicate setup=%v", err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if transport.refreshes.Load() != 1 {
		t.Fatal("discovery was not refreshed after setup")
	}
	if _, err := app.GetDeviceSnapshot(); err != nil {
		t.Fatal("normal snapshot did not resume")
	}
}

func TestMockAppDoesNotExposeNativeSetup(t *testing.T) {
	app := NewAppWithTransport(backend.NewMockTransport())
	if app.OnboardingCapabilities().Available {
		t.Fatal("mock mode exposed native provisioning")
	}
	if _, err := app.ScanSetupLights(false); !errors.Is(err, backend.ErrOnboardingUnavailable) {
		t.Fatalf("scan=%v", err)
	}
}

func TestSetupCanBeCancelledBeforeBackendPreparationOrWhileWaitingForControls(t *testing.T) {
	transport := &setupRecordingTransport{}
	app := NewAppWithTransport(transport)
	app.onboarding = &recordingOnboarding{provision: func(context.Context, backend.SetupLightRequest, backend.SetupVerifier, func(backend.SetupProgress)) (backend.SetupLightResult, error) {
		t.Error("cancelled setup entered native provisioning")
		return backend.SetupLightResult{}, nil
	}}
	app.CancelLightSetup("early")
	if result, err := app.SetupLight(backend.SetupLightRequest{AttemptID: "early"}); err != nil || !result.Cancelled {
		t.Fatalf("early cancel=%#v %v", result, err)
	}
	prepared := make(chan struct{})
	app.emitEvent = func(context.Context, string, ...interface{}) { close(prepared) }
	app.controlGate.RLock()
	done := make(chan backend.SetupLightResult, 1)
	go func() { result, _ := app.SetupLight(backend.SetupLightRequest{AttemptID: "waiting"}); done <- result }()
	<-prepared
	app.CancelLightSetup("waiting")
	app.controlGate.RUnlock()
	if result := <-done; !result.Cancelled {
		t.Fatal("preparation cancellation ignored")
	}
	if transport.refreshes.Load() != 0 {
		t.Fatal("cancelled preparation touched network discovery")
	}
}
