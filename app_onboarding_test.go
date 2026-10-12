package main

import (
	"context"
	"testing"

	"hikari-desktop/internal/backend"
)

type recordingOnboarding struct {
	scans, cancels, closes int
}

func (*recordingOnboarding) Capabilities() backend.OnboardingCapabilities {
	return backend.OnboardingCapabilities{Available: true}
}
func (s *recordingOnboarding) Scan(context.Context) (backend.SetupLightDiscovery, error) {
	s.scans++
	return backend.SetupLightDiscovery{Lights: []backend.SetupLight{}}, nil
}
func (s *recordingOnboarding) CancelScan()                 { s.cancels++ }
func (s *recordingOnboarding) Close(context.Context) error { s.closes++; return nil }

func TestAppOnboardingIsLazyAndClosedOnShutdown(t *testing.T) {
	app := NewAppWithTransport(&recordingTransport{})
	setup := &recordingOnboarding{}
	app.onboarding = setup
	app.startup(context.Background())
	if !app.OnboardingCapabilities().Available || setup.scans != 0 {
		t.Fatal("startup scanned for setup devices")
	}
	if _, err := app.ScanSetupLights(); err != nil || setup.scans != 1 {
		t.Fatal("explicit setup scan did not delegate")
	}
	app.CancelOnboardingScan()
	app.shutdown(context.Background())
	if setup.cancels != 1 || setup.closes != 1 {
		t.Fatal("setup resources were not cancelled/closed")
	}
}
