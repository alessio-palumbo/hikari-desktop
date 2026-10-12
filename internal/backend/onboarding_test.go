package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeOnboardingScanner struct {
	scan func(context.Context) (SetupLightDiscovery, error)
}

func (f fakeOnboardingScanner) Scan(ctx context.Context) (SetupLightDiscovery, error) {
	return f.scan(ctx)
}

func TestOnboardingUnavailableIsInert(t *testing.T) {
	s := &OnboardingService{}
	if s.Capabilities().Available {
		t.Fatal("unsupported setup was advertised")
	}
	if result, err := s.Scan(context.Background()); !errors.Is(err, ErrOnboardingUnavailable) || result.Lights == nil {
		t.Fatalf("scan=%#v %v", result, err)
	}
	s.CancelScan()
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOnboardingScanIsLazyBoundedAndSorted(t *testing.T) {
	calls := 0
	s := &OnboardingService{scanner: fakeOnboardingScanner{scan: func(ctx context.Context) (SetupLightDiscovery, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("scan has no deadline")
		}
		return SetupLightDiscovery{CurrentSSID: "Home", Lights: []SetupLight{{Serial: "b", SSID: "LIFX B"}, {Serial: "a", SSID: "LIFX A"}}}, nil
	}}}
	if !s.Capabilities().Available || calls != 0 {
		t.Fatal("capabilities scanned the network")
	}
	result, err := s.Scan(context.Background())
	if err != nil || calls != 1 || result.CurrentSSID != "Home" || result.Lights[0].Serial != "a" {
		t.Fatalf("scan=%#v %v", result, err)
	}
}

func TestOnboardingScanCancelAndRetry(t *testing.T) {
	started := make(chan struct{})
	calls := 0
	s := &OnboardingService{scanner: fakeOnboardingScanner{scan: func(ctx context.Context) (SetupLightDiscovery, error) {
		calls++
		if calls == 1 {
			close(started)
			<-ctx.Done()
			return SetupLightDiscovery{}, ctx.Err()
		}
		return SetupLightDiscovery{}, nil
	}}}
	result := make(chan error, 1)
	go func() { _, err := s.Scan(context.Background()); result <- err }()
	<-started
	if _, err := s.Scan(context.Background()); !errors.Is(err, ErrOnboardingBusy) {
		t.Fatalf("concurrent scan=%v", err)
	}
	s.CancelScan()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation=%v", err)
	}
	if got, err := s.Scan(context.Background()); err != nil || !reflect.DeepEqual(got.Lights, []SetupLight{}) {
		t.Fatalf("retry=%#v %v", got, err)
	}
}

func TestOnboardingCloseCancelsAndWaitsForScan(t *testing.T) {
	started := make(chan struct{})
	s := &OnboardingService{scanner: fakeOnboardingScanner{scan: func(ctx context.Context) (SetupLightDiscovery, error) {
		close(started)
		<-ctx.Done()
		return SetupLightDiscovery{}, ctx.Err()
	}}}
	result := make(chan error, 1)
	go func() { _, err := s.Scan(context.Background()); result <- err }()
	<-started
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("close scan=%v", err)
	}
	if _, err := s.Scan(context.Background()); !errors.Is(err, ErrOnboardingClosed) {
		t.Fatalf("scan after close=%v", err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestOnboardingCancelledRequestDoesNotCallScanner(t *testing.T) {
	s := &OnboardingService{scanner: fakeOnboardingScanner{scan: func(context.Context) (SetupLightDiscovery, error) {
		t.Fatal("cancelled request scanned")
		return SetupLightDiscovery{}, nil
	}}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.Scan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled scan=%v", err)
	}
}

func TestOnboardingScanFailureAllowsAnotherAttempt(t *testing.T) {
	sentinel := errors.New("Wi-Fi unavailable")
	s := &OnboardingService{scanner: fakeOnboardingScanner{scan: func(context.Context) (SetupLightDiscovery, error) { return SetupLightDiscovery{}, sentinel }}}
	for range 2 {
		if _, err := s.Scan(context.Background()); !errors.Is(err, sentinel) {
			t.Fatalf("scan=%v", err)
		}
	}
}

func TestOnboardingCancellationDoesNotAbandonUninterruptibleNativeScan(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	s := &OnboardingService{scanner: fakeOnboardingScanner{scan: func(context.Context) (SetupLightDiscovery, error) {
		close(started)
		<-release
		return SetupLightDiscovery{}, nil
	}}}
	result := make(chan error, 1)
	go func() { _, err := s.Scan(context.Background()); result <- err }()
	<-started
	s.CancelScan()
	if _, err := s.Scan(context.Background()); !errors.Is(err, ErrOnboardingBusy) {
		t.Fatalf("native scan still active: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("bounded close=%v", err)
	}
	close(release)
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled native result=%v", err)
	}
}
