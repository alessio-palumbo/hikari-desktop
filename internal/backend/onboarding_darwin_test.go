//go:build darwin && corewlan && cgo

package backend

import "testing"

func TestOnboardingMacOSBuildAdvertisesSetupWithoutScanning(t *testing.T) {
	if !NewOnboardingService().Capabilities().Available {
		t.Fatal("macOS CoreWLAN build did not expose setup")
	}
}
