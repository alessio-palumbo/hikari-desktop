package backend

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultCommandEngineSettingsHonorsEnv(t *testing.T) {
	t.Setenv("HIKARI_COMMANDS_ENABLED", "true")
	t.Setenv("HIKARI_COMMAND_ENGINE_PATH", "/tmp/engine")
	t.Setenv("HIKARI_COMMAND_ENGINE_CONFIG", "/tmp/config.json")

	settings := defaultCommandEngineSettings()

	if !settings.Enabled || settings.EnginePath != "/tmp/engine" || settings.ConfigPath != "/tmp/config.json" {
		t.Fatalf("settings = %#v", settings)
	}
}

func TestFileCommandSettingsStoreEnvOverridesPersistedPath(t *testing.T) {
	t.Setenv("HIKARI_COMMAND_ENGINE_PATH", "/tmp/current-engine")
	path := filepath.Join(t.TempDir(), "command-settings.json")
	if err := os.WriteFile(path, []byte(`{"enabled":true,"enginePath":"/tmp/old-engine"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := fileCommandSettingsStore{path: path}.LoadCommandEngineSettings()
	if err != nil {
		t.Fatalf("LoadCommandEngineSettings returned error: %v", err)
	}
	if settings.EnginePath != "/tmp/current-engine" {
		t.Fatalf("EnginePath = %q", settings.EnginePath)
	}
}

func TestCommandEngineCommandResolvesExplicitPath(t *testing.T) {
	path, args, err := commandEngineCommand(CommandEngineSettings{Enabled: true, EnginePath: "/tmp/engine", ConfigPath: "/tmp/config.json"})
	if err != nil {
		t.Fatalf("commandEngineCommand returned error: %v", err)
	}
	if path != "/tmp/engine" {
		t.Fatalf("path = %q", path)
	}
	if len(args) != 3 || args[0] != "serve" || args[1] != "-config" || args[2] != "/tmp/config.json" {
		t.Fatalf("args = %#v", args)
	}
}

func TestBundledCommandEnginePathFindsExecutableBesideBinary(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "lifx-command-engine"
	if filepath.Ext(exe) == ".exe" {
		name += ".exe"
	}
	path := filepath.Join(filepath.Dir(exe), name)
	if err := os.WriteFile(path, []byte("test"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	got, ok := bundledCommandEnginePath()
	if !ok || got != path {
		t.Fatalf("bundledCommandEnginePath = %q, %v; want %q, true", got, ok, path)
	}
}
