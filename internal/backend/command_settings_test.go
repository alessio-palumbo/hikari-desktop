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

func TestCommandEngineCommandAddsWhisperEnvFlags(t *testing.T) {
	t.Setenv("HIKARI_WHISPER_COMMAND", "/tmp/whisper-cli")
	t.Setenv("HIKARI_WHISPER_MODEL", "/tmp/model.bin")
	t.Setenv("HIKARI_WHISPER_ARGS", "-ng --no-timestamps")
	path, args, err := commandEngineCommand(CommandEngineSettings{Enabled: true, EnginePath: "/tmp/engine"})
	if err != nil {
		t.Fatalf("commandEngineCommand returned error: %v", err)
	}
	if path != "/tmp/engine" {
		t.Fatalf("path = %q", path)
	}
	want := []string{"serve", "-whisper-command", "/tmp/whisper-cli", "-whisper-model", "/tmp/model.bin", "-whisper-arg", "-ng", "-whisper-arg", "--no-timestamps"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v", args)
		}
	}
}

func TestCommandEngineCommandAddsWhisperJSONArgs(t *testing.T) {
	t.Setenv("HIKARI_WHISPER_COMMAND", "/tmp/whisper-cli")
	t.Setenv("HIKARI_WHISPER_MODEL", "/tmp/model.bin")
	t.Setenv("HIKARI_WHISPER_ARGS", `["-ng","--prompt","turn tv off, set desk warm white"]`)
	_, args, err := commandEngineCommand(CommandEngineSettings{Enabled: true, EnginePath: "/tmp/engine"})
	if err != nil {
		t.Fatalf("commandEngineCommand returned error: %v", err)
	}
	want := []string{"serve", "-whisper-command", "/tmp/whisper-cli", "-whisper-model", "/tmp/model.bin", "-whisper-arg", "-ng", "-whisper-arg", "--prompt", "-whisper-arg", "turn tv off, set desk warm white"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v", args)
		}
	}
}

func TestCommandEngineCommandRejectsInvalidWhisperJSONArgs(t *testing.T) {
	t.Setenv("HIKARI_WHISPER_ARGS", `[`)
	if _, _, err := commandEngineCommand(CommandEngineSettings{Enabled: true, EnginePath: "/tmp/engine"}); err == nil {
		t.Fatal("commandEngineCommand returned nil error")
	}
}

func TestCommandEngineSettingsReportsTranscriptionWhenWhisperEnvConfigured(t *testing.T) {
	t.Setenv("HIKARI_WHISPER_COMMAND", "/tmp/whisper-cli")
	t.Setenv("HIKARI_WHISPER_MODEL", "/tmp/model.bin")
	service := NewCommandEngineServiceWithStore(&memoryCommandSettingsStore{
		settings: CommandEngineSettings{Enabled: true, EnginePath: "/tmp/engine"},
	})

	settings, err := service.Settings(nil)
	if err != nil {
		t.Fatalf("Settings returned error: %v", err)
	}
	if !settings.Transcription {
		t.Fatalf("Transcription = false; want true")
	}
}

func TestCommandEngineSettingsDoesNotReportTranscriptionWithPartialWhisperEnv(t *testing.T) {
	t.Setenv("HIKARI_WHISPER_COMMAND", "/tmp/whisper-cli")
	service := NewCommandEngineServiceWithStore(&memoryCommandSettingsStore{
		settings: CommandEngineSettings{Enabled: true, EnginePath: "/tmp/engine"},
	})

	settings, err := service.Settings(nil)
	if err != nil {
		t.Fatalf("Settings returned error: %v", err)
	}
	if settings.Transcription {
		t.Fatalf("Transcription = true; want false")
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
