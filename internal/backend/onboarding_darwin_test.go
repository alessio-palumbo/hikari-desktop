//go:build darwin && corewlan && cgo

package backend

import (
	"context"
	"errors"
	"testing"

	onboard "github.com/alessio-palumbo/lifx-onboard"
	"github.com/alessio-palumbo/lifx-onboard/scanner"
)

func TestSetupScanRetriesTransientFailures(t *testing.T) {
	for _, test := range []struct {
		name      string
		failure   error
		wantCalls int
	}{
		{"transient", errors.New("radio busy"), 3},
		{"permission", scanner.ErrRedacted, 1},
		{"unsupported", scanner.ErrUnsupportedPlatform, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, waits := 0, 0
			_, err := scanSetupNetworks(context.Background(), func() ([]scanner.AP, error) {
				calls++
				if calls == 3 {
					return []scanner.AP{}, nil
				}
				return nil, test.failure
			}, func(context.Context) error { waits++; return nil })
			if calls != test.wantCalls || waits != test.wantCalls-1 {
				t.Fatalf("calls=%d waits=%d", calls, waits)
			}
			if test.wantCalls == 3 && err != nil {
				t.Fatal(err)
			}
			if test.wantCalls == 1 && !errors.Is(err, test.failure) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestSetupScanRetryCancellationAndLimit(t *testing.T) {
	failure := errors.New("Wi-Fi unavailable")
	calls := 0
	_, err := scanSetupNetworks(context.Background(), func() ([]scanner.AP, error) { calls++; return nil, failure }, func(context.Context) error { return nil })
	if calls != 3 || !errors.Is(err, failure) {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
	calls = 0
	_, err = scanSetupNetworks(context.Background(), func() ([]scanner.AP, error) { calls++; return nil, failure }, func(context.Context) error { return context.Canceled })
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestOnboardingMacOSBuildAdvertisesSetupWithoutScanning(t *testing.T) {
	if !NewOnboardingService().Capabilities().Available {
		t.Fatal("macOS CoreWLAN build did not expose setup")
	}
}

func TestMacOSSetupDelegatesToLibraryWithoutNativeWiFiInTests(t *testing.T) {
	scanner := macOSOnboardingScanner{provision: func(_ context.Context, opts onboard.OnboardBySSIDOptions) (onboard.Result, error) {
		if opts.Scanner == nil || opts.Joiner == nil || opts.Sender == nil {
			t.Fatal("library dependencies missing")
		}
		if opts.BulbSSID != testSetupRequest().BulbSSID || opts.Password != testSetupRequest().Password || opts.Security != onboard.SecurityWpa2AesPsk {
			t.Fatal("wrong provisioning options")
		}
		opts.Events <- onboard.EventConfigured{}
		opts.Events <- onboard.EventRejoined{}
		return onboard.Result{Verified: true}, nil
	}}
	var phases []string
	result := scanner.Provision(context.Background(), testSetupRequest(), emptySetupVerifier{}, func(phase string) { phases = append(phases, phase) })
	if !result.Configured || !result.Verified || len(phases) != 2 {
		t.Fatalf("setup=%#v phases=%v", result, phases)
	}
}

func TestMacOSSetupReportsCancellationAndPartialConfiguration(t *testing.T) {
	scanner := macOSOnboardingScanner{provision: func(_ context.Context, opts onboard.OnboardBySSIDOptions) (onboard.Result, error) {
		opts.Events <- onboard.EventConfigured{}
		return onboard.Result{Phase: onboard.PhaseVerify}, context.Canceled
	}}
	result := scanner.Provision(context.Background(), testSetupRequest(), emptySetupVerifier{}, func(string) {})
	if !result.Cancelled || !result.Configured || result.Verified || result.Error == "" {
		t.Fatalf("cancel=%#v", result)
	}
	scanner.provision = func(context.Context, onboard.OnboardBySSIDOptions) (onboard.Result, error) {
		return onboard.Result{Phase: onboard.PhaseJoin}, errors.New("secret-password-in-raw-error")
	}
	result = scanner.Provision(context.Background(), testSetupRequest(), emptySetupVerifier{}, func(string) {})
	if result.Error == "secret-password-in-raw-error" || result.Configured {
		t.Fatal("unsafe/misleading library error surfaced")
	}
}
