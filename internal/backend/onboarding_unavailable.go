//go:build !linux && !windows && (!darwin || !corewlan || !cgo)

package backend

// Unsupported platforms and macOS builds without CoreWLAN stay disabled.
// The stub intentionally has no dependency on platform-specific Wi-Fi APIs.
func newPlatformOnboardingScanner() onboardingScanner { return nil }
