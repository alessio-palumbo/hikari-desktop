//go:build !darwin || !corewlan || !cgo

package backend

// Linux and Windows stay disabled until their Wi-Fi setup path is validated.
// The stub intentionally has no dependency on platform-specific Wi-Fi APIs.
func newPlatformOnboardingScanner() onboardingScanner { return nil }
