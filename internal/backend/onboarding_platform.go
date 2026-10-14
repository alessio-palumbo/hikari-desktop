//go:build linux || windows || (darwin && corewlan && cgo)

package backend

import (
	"context"
	"errors"
	"time"

	onboard "github.com/alessio-palumbo/lifx-onboard"
	"github.com/alessio-palumbo/lifx-onboard/scanner"
)

type platformOnboardingScanner struct {
	provision func(context.Context, onboard.OnboardBySSIDOptions) (onboard.Result, error)
}

func newPlatformOnboardingScanner() onboardingScanner { return platformOnboardingScanner{} }

func (platformOnboardingScanner) Scan(ctx context.Context) (SetupLightDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	// Requested only for an explicit setup scan, never during application startup.
	// Native Location/CoreWLAN calls cannot be interrupted by a Go context;
	// cancellation is checked before and after each call rather than abandoning
	// native work in a background goroutine.
	if err := prepareOnboardingScan(); err != nil {
		return SetupLightDiscovery{}, err
	}
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	wifi := scanner.NewDefaultScanner("")
	reporter, _ := wifi.(scanner.CurrentNetworkReporter)
	if reporter == nil {
		// Linux reports the active SSID through its joiner, not its scanner.
		reporter, _ = scanner.NewDefaultJoiner("").(scanner.CurrentNetworkReporter)
	}
	return scanOnboardingWiFi(ctx, wifi, reporter)
}

func scanOnboardingWiFi(ctx context.Context, wifi scanner.Scanner, reporter scanner.CurrentNetworkReporter) (SetupLightDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	current := ""
	if reporter != nil {
		var err error
		current, err = reporter.CurrentSSID(ctx)
		if err != nil {
			if errors.Is(err, scanner.ErrRedacted) {
				return SetupLightDiscovery{}, onboardingScanError(true)
			}
			return SetupLightDiscovery{}, errors.New("could not read the current Wi-Fi network")
		}
	}
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	aps, err := scanSetupNetworks(ctx, func() ([]scanner.AP, error) {
		return wifi.Scan(ctx, scanner.ScanOptions{IncludeNonLIFX: true})
	}, waitForSetupScanRetry)
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	if errors.Is(err, scanner.ErrRedacted) {
		return SetupLightDiscovery{}, onboardingScanError(true)
	}
	if err != nil {
		return SetupLightDiscovery{}, onboardingScanError(false)
	}
	result := SetupLightDiscovery{Lights: []SetupLight{}, CurrentSSID: current}
	for _, ap := range aps {
		if !ap.Serial.IsNil() {
			result.Lights = append(result.Lights, SetupLight{Serial: ap.Serial.String(), SSID: ap.SSID, RSSI: ap.RSSI})
		} else if ap.SSID != "" {
			result.Networks = append(result.Networks, ap.SSID)
		}
	}
	return result, nil
}

// CoreWLAN can reject back-to-back scans while its radio scan is still busy.
// Retry briefly before surfacing a failure; permission failures are not transient.
func scanSetupNetworks(ctx context.Context, scan func() ([]scanner.AP, error), wait func(context.Context) error) ([]scanner.AP, error) {
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		aps, err := scan()
		if err == nil || attempt == 2 || errors.Is(err, scanner.ErrRedacted) || errors.Is(err, scanner.ErrUnsupportedPlatform) {
			return aps, err
		}
		if err := wait(ctx); err != nil {
			return nil, err
		}
	}
}

func waitForSetupScanRetry(ctx context.Context) error {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s platformOnboardingScanner) Provision(ctx context.Context, req SetupLightRequest, verifier SetupVerifier, progress func(string)) SetupLightResult {
	events := make(chan onboard.Event, 128)
	done := make(chan struct{})
	configured := false
	go func() {
		defer close(done)
		for event := range events {
			phase := ""
			switch event.(type) {
			case onboard.EventJoining, onboard.EventJoined:
				phase = "join"
			case onboard.EventWaitingForSoftAP:
				phase = "wait"
			case onboard.EventOnSoftAP, onboard.EventDialing:
				phase = "dial"
			case onboard.EventConfigured:
				configured = true
				phase = "configure"
			case onboard.EventRejoining, onboard.EventRejoined:
				phase = "rejoin"
			case onboard.EventVerifying, onboard.EventVerified:
				phase = "verify"
			}
			if phase != "" {
				progress(phase)
			}
		}
	}()
	security := onboard.SecurityWpa2AesPsk
	if req.OpenNetwork {
		security = onboard.SecurityOpen
	}
	// The library owns provisioning and host-network recovery, including recovery
	// after cancellation. Pass both adapters explicitly; do not drive Wi-Fi locally.
	run := s.provision
	if run == nil {
		run = onboard.OnboardBySSID
	}
	res, err := run(ctx, onboard.OnboardBySSIDOptions{
		Options: onboard.Options{SSID: req.SSID, Password: req.Password, Security: security,
			Scanner: scanner.NewDefaultScanner(""), Joiner: scanner.NewDefaultJoiner(""),
			Sender: verifier, VerifyTimeout: time.Minute, Events: events},
		BulbSSID: req.BulbSSID,
	})
	close(events)
	<-done
	result := SetupLightResult{Configured: configured || err == nil, Verified: res.Verified}
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			result.Cancelled = true
			result.Error = "Setup cancelled. Your original Wi-Fi connection has been restored where possible."
			if result.Configured {
				result.Error = "Setup cancelled after Wi-Fi details were sent. Check LAN discovery before retrying."
			}
		case errors.Is(err, context.DeadlineExceeded):
			result.Error = "Setup timed out. Check the device and your Wi-Fi connection."
		case errors.Is(err, onboard.ErrVerifyTimeout):
			result.Error = "Wi-Fi details were sent, but the device was not found on this LAN. Check the network and password before retrying."
		case res.Phase == onboard.PhaseRejoin:
			result.Error = "Wi-Fi details were sent, but this computer could not reconnect. Reconnect to your normal Wi-Fi network."
		case res.Phase == onboard.PhaseJoin:
			result.Error = "Could not join the device's setup network. Check that it is still in setup mode."
		case res.Phase == onboard.PhaseWait || res.Phase == onboard.PhaseDial:
			result.Error = "The device's setup connection did not respond. Check the device and try scanning again."
		default:
			result.Error = "Could not complete device setup. Check the device and Wi-Fi credentials, then scan again."
		}
	}
	return result
}
