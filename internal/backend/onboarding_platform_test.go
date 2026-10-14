//go:build linux || windows || (darwin && corewlan && cgo)

package backend

import (
	"context"
	"errors"
	"reflect"
	"testing"

	onboard "github.com/alessio-palumbo/lifx-onboard"
	"github.com/alessio-palumbo/lifx-onboard/scanner"
)

type setupWiFiFixture struct{ options scanner.ScanOptions }

func (f *setupWiFiFixture) Interface() string { return "test" }
func (f *setupWiFiFixture) Scan(_ context.Context, options scanner.ScanOptions) ([]scanner.AP, error) {
	f.options = options
	ap, _ := scanner.APFromSSID("LIFX Candle aabbcc", false)
	ap.RSSI = -42
	return []scanner.AP{ap, {SSID: "Home"}, {SSID: "Office"}, {}}, nil
}
func (*setupWiFiFixture) CurrentSSID(context.Context) (string, error) { return "Home", nil }

func TestPlatformScanMapsPublicAPsAndCurrentNetwork(t *testing.T) {
	wifi := &setupWiFiFixture{}
	result, err := scanOnboardingWiFi(context.Background(), wifi, wifi)
	if err != nil {
		t.Fatal(err)
	}
	if !wifi.options.IncludeNonLIFX || result.CurrentSSID != "Home" || !reflect.DeepEqual(result.Networks, []string{"Home", "Office"}) {
		t.Fatalf("discovery=%+v options=%+v", result, wifi.options)
	}
	if len(result.Lights) != 1 || result.Lights[0].Serial != "d073d5aabbcc" || result.Lights[0].RSSI != -42 {
		t.Fatalf("lights=%+v", result.Lights)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scanOnboardingWiFi(ctx, wifi, wifi); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled=%v", err)
	}
}

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

func TestOnboardingSupportedBuildAdvertisesSetupWithoutScanning(t *testing.T) {
	if !NewOnboardingService().Capabilities().Available {
		t.Fatal("supported build did not expose setup")
	}
}

func TestPlatformSetupDelegatesToLibraryWithoutNativeWiFiInTests(t *testing.T) {
	scanner := platformOnboardingScanner{provision: func(_ context.Context, opts onboard.OnboardBySSIDOptions) (onboard.Result, error) {
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

func TestPlatformSetupReportsCancellationAndPartialConfiguration(t *testing.T) {
	scanner := platformOnboardingScanner{provision: func(_ context.Context, opts onboard.OnboardBySSIDOptions) (onboard.Result, error) {
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
