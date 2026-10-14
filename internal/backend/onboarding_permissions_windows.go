package backend

import "errors"

func prepareOnboardingScan() error { return nil }

func onboardingScanError(bool) error {
	return errors.New("could not scan Wi-Fi networks; check Wi-Fi and Windows Location access, including 'Let desktop apps access your location', then try again")
}
