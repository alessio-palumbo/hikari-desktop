//go:build darwin && corewlan && cgo

package backend

import (
	"context"
	"errors"

	onboard "github.com/alessio-palumbo/lifx-onboard"
	"github.com/alessio-palumbo/lifx-onboard/scanner"
)

type macOSOnboardingScanner struct{}

func newPlatformOnboardingScanner() onboardingScanner { return macOSOnboardingScanner{} }

func (macOSOnboardingScanner) Scan(ctx context.Context) (SetupLightDiscovery, error) {
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	// Requested only for an explicit setup scan, never during application startup.
	// Native Location/CoreWLAN calls cannot be interrupted by a Go context;
	// cancellation is checked before and after each call rather than abandoning
	// native work in a background goroutine.
	if !onboard.RequestMacOSLocationAccess().Granted() {
		return SetupLightDiscovery{}, errors.New("allow hikari Location access in System Settings to find lights in setup mode; use the packaged macOS app")
	}
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	wifi := scanner.NewDefaultScanner("")
	current := ""
	if reporter, ok := wifi.(scanner.CurrentNetworkReporter); ok {
		var err error
		current, err = reporter.CurrentSSID(ctx)
		if err != nil {
			return SetupLightDiscovery{}, errors.New("could not read the current Wi-Fi network")
		}
	}
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	aps, err := wifi.Scan(ctx, scanner.ScanOptions{})
	if err := ctx.Err(); err != nil {
		return SetupLightDiscovery{}, err
	}
	if errors.Is(err, scanner.ErrRedacted) {
		return SetupLightDiscovery{}, errors.New("Wi-Fi names are hidden; allow hikari Location access and open the packaged macOS app")
	}
	if err != nil {
		return SetupLightDiscovery{}, errors.New("could not scan Wi-Fi networks; check that Wi-Fi is enabled and try again")
	}
	result := SetupLightDiscovery{Lights: []SetupLight{}, CurrentSSID: current}
	for _, ap := range aps {
		if !ap.Serial.IsNil() {
			result.Lights = append(result.Lights, SetupLight{Serial: ap.Serial.String(), SSID: ap.SSID, RSSI: ap.RSSI})
		}
	}
	return result, nil
}
