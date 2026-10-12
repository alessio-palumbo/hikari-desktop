package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestOnboardingReopenReusesRecentScanButRefreshDoesNot(t *testing.T) {
	calls := 0
	s := &OnboardingService{scanner: fakeOnboardingScanner{scan: func(context.Context) (SetupLightDiscovery, error) {
		calls++
		return SetupLightDiscovery{Lights: []SetupLight{{SSID: "LIFX A"}}, Networks: []string{"Home"}}, nil
	}}}
	first, err := s.Discover(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	first.Lights[0].SSID = "mutated"
	first.Networks[0] = "mutated"
	second, err := s.Discover(context.Background(), false)
	if err != nil || calls != 1 || second.Lights[0].SSID != "LIFX A" || second.Networks[0] != "Home" {
		t.Fatalf("cached=%+v calls=%d err=%v", second, calls, err)
	}
	if _, err := s.Discover(context.Background(), true); err != nil || calls != 2 {
		t.Fatalf("refresh calls=%d err=%v", calls, err)
	}
	s.lastScanAt = time.Now().Add(-11 * time.Second)
	if _, err := s.Discover(context.Background(), false); err != nil || calls != 3 {
		t.Fatalf("expired calls=%d err=%v", calls, err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Discover(context.Background(), false); !errors.Is(err, ErrOnboardingClosed) {
		t.Fatalf("closed=%v", err)
	}
}

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
		return SetupLightDiscovery{CurrentSSID: "Home", Networks: []string{"z", "Home", "Home", "", "a"}, Lights: []SetupLight{{Serial: "b", SSID: "LIFX B"}, {Serial: "a", SSID: "LIFX A"}}}, nil
	}}}
	if !s.Capabilities().Available || calls != 0 {
		t.Fatal("capabilities scanned the network")
	}
	result, err := s.Scan(context.Background())
	if err != nil || calls != 1 || result.CurrentSSID != "Home" || result.Lights[0].Serial != "a" {
		t.Fatalf("scan=%#v %v", result, err)
	}
	if !reflect.DeepEqual(result.Networks, []string{"Home", "a", "z"}) {
		t.Fatalf("networks=%v", result.Networks)
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

func TestOnboardingReopenWaitsForCancelledNativeScan(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	calls := 0
	s := &OnboardingService{scanner: fakeOnboardingScanner{scan: func(ctx context.Context) (SetupLightDiscovery, error) {
		calls++
		if calls == 1 {
			close(started)
			<-release
		}
		return SetupLightDiscovery{}, ctx.Err()
	}}}
	first := make(chan error, 1)
	go func() { _, err := s.Scan(context.Background()); first <- err }()
	<-started
	s.CancelScan()
	// A reopened request may wait, but must remain cancellable itself.
	ctx, cancel := context.WithCancel(context.Background())
	second := make(chan error, 1)
	go func() { _, err := s.Scan(ctx); second <- err }()
	cancel()
	if err := <-second; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting scan=%v", err)
	}
	third := make(chan error, 1)
	go func() { _, err := s.Scan(context.Background()); third <- err }()
	close(release)
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("first scan=%v", err)
	}
	if err := <-third; err != nil {
		t.Fatalf("reopened scan=%v", err)
	}
	if calls != 2 {
		t.Fatalf("scan calls=%d", calls)
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
	s.mu.Lock()
	stillRunning := s.done != nil
	s.mu.Unlock()
	if !stillRunning {
		t.Fatal("cancel abandoned native scan")
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
