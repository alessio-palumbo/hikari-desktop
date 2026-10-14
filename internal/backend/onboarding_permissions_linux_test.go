package backend

import (
	"strings"
	"testing"
)

func TestLinuxSetupExplainsMissingNetworkManager(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := prepareOnboardingScan(); err == nil || !strings.Contains(err.Error(), "NetworkManager (nmcli)") {
		t.Fatalf("missing prerequisite=%v", err)
	}
}
