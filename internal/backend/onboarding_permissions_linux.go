package backend

import (
	"errors"
	"os/exec"
)

func prepareOnboardingScan() error {
	if _, err := exec.LookPath("nmcli"); err != nil {
		return errors.New("device setup requires NetworkManager (nmcli); install it and try again")
	}
	return nil
}

func onboardingScanError(bool) error {
	return errors.New("could not scan Wi-Fi networks; check that Wi-Fi is enabled and managed by NetworkManager, then try again")
}
