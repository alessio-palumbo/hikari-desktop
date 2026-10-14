//go:build darwin && corewlan && cgo

package backend

import (
	"errors"
	onboard "github.com/alessio-palumbo/lifx-onboard"
)

func prepareOnboardingScan() error {
	if !onboard.RequestMacOSLocationAccess().Granted() {
		return errors.New("allow hikari Location access in System Settings to find devices in setup mode; use the packaged macOS app")
	}
	return nil
}

func onboardingScanError(redacted bool) error {
	if redacted {
		return errors.New("Wi-Fi names are hidden; allow hikari Location access and open the packaged macOS app")
	}
	return errors.New("could not scan Wi-Fi networks; check that Wi-Fi is enabled and try again")
}
