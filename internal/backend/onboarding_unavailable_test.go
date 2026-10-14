//go:build !linux && !windows && (!darwin || !corewlan || !cgo)

package backend

import "testing"

func TestOnboardingDisabledWithoutValidatedPlatformBackend(t *testing.T) {
	if NewOnboardingService().Capabilities().Available {
		t.Fatal("unvalidated build exposed light setup")
	}
}
