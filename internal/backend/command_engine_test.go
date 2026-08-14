package backend

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"testing"
	"time"

	commandclient "github.com/alessio-palumbo/lifx-command-engine/client"
)

func TestCommandEngineServiceInterpretsThroughClient(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	if err := store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: "/bin/engine"}); err != nil {
		t.Fatal(err)
	}
	client := &fakeCommandEngineClient{
		capabilities: compatibleCommandCapabilities(),
		plan: commandclient.CommandPlan{
			SchemaVersion:     "1",
			Confidence:        0.95,
			ConfidenceResult:  commandclient.ConfidenceResult{Level: "high", Reasons: []string{"exact label"}},
			NeedsConfirmation: false,
			Summary:           "Turn on Ceiling",
			Commands: []commandclient.CommandIntent{{
				Targets: []commandclient.TargetRef{{Serial: "d0:73:d5:01:a2:c3", Label: "Ceiling"}},
				Action:  commandclient.Action{Power: boolPtr(true)},
			}},
		},
	}
	service := NewCommandEngineServiceWithStore(store)
	service.newClient = func(config commandclient.Config) (commandEngineClient, error) {
		if config.Path != "/bin/engine" {
			t.Fatalf("path = %q", config.Path)
		}
		if !config.RestartOnCrash {
			t.Fatal("RestartOnCrash = false")
		}
		return client, nil
	}

	preview, err := service.Interpret(context.Background(), "turn ceiling on", MockDeviceSnapshot())
	if err != nil {
		t.Fatalf("Interpret returned error: %v", err)
	}
	if !client.started || !client.capabilitiesCalled || client.input.Text != "turn ceiling on" {
		t.Fatalf("client state = %#v", client)
	}
	if preview.Summary != "Turn on Ceiling" || preview.Empty || len(preview.Commands) != 1 {
		t.Fatalf("preview = %#v", preview)
	}
	if preview.Commands[0].Targets[0].Serial != "d0:73:d5:01:a2:c3" || preview.Commands[0].Action.Power == nil || !*preview.Commands[0].Action.Power {
		t.Fatalf("preview command = %#v", preview.Commands[0])
	}
}

func TestCommandEngineServiceRejectsDisabled(t *testing.T) {
	service := NewCommandEngineServiceWithStore(&memoryCommandSettingsStore{})
	if _, err := service.Interpret(context.Background(), "turn ceiling on", MockDeviceSnapshot()); err == nil {
		t.Fatal("Interpret returned nil error, want disabled error")
	}
}

func TestCommandEngineServiceRejectsUnknownTarget(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: "/bin/engine"})
	service := NewCommandEngineServiceWithStore(store)
	service.newClient = func(config commandclient.Config) (commandEngineClient, error) {
		return &fakeCommandEngineClient{
			capabilities: compatibleCommandCapabilities(),
			plan: commandclient.CommandPlan{
				SchemaVersion: "1",
				Summary:       "Turn on missing",
				Commands: []commandclient.CommandIntent{{
					Targets: []commandclient.TargetRef{{Serial: "missing"}},
					Action:  commandclient.Action{Power: boolPtr(true)},
				}},
			},
		}, nil
	}
	if _, err := service.Interpret(context.Background(), "turn missing on", MockDeviceSnapshot()); err == nil {
		t.Fatal("Interpret returned nil error, want unknown target error")
	}
}

func TestCommandEngineServiceSkipsSwitchTargets(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: "/bin/engine"})
	service := NewCommandEngineServiceWithStore(store)
	service.newClient = func(config commandclient.Config) (commandEngineClient, error) {
		return &fakeCommandEngineClient{
			capabilities: compatibleCommandCapabilities(),
			plan: commandclient.CommandPlan{
				SchemaVersion: "1",
				Summary:       "Turn Desk off",
				Commands: []commandclient.CommandIntent{{
					Targets: []commandclient.TargetRef{{Serial: "d0:73:d5:01:a2:c3"}, {Serial: "d0:73:d5:02:b1:20"}},
					Action:  commandclient.Action{Power: boolPtr(false)},
				}},
			},
		}, nil
	}
	preview, err := service.Interpret(context.Background(), "turn desk off", MockDeviceSnapshot())
	if err != nil {
		t.Fatalf("Interpret returned error: %v", err)
	}
	if preview.Empty || len(preview.Commands) != 1 || len(preview.Commands[0].Targets) != 1 {
		t.Fatalf("preview = %#v, want switch skipped and light retained", preview)
	}
	if preview.Commands[0].Targets[0].Serial != "d0:73:d5:01:a2:c3" {
		t.Fatalf("targets = %#v", preview.Commands[0].Targets)
	}
	if len(preview.SkippedTargets) != 1 || preview.SkippedTargets[0].Serial != "d0:73:d5:02:b1:20" {
		t.Fatalf("skipped = %#v", preview.SkippedTargets)
	}
}

func TestCommandEngineServiceReturnsEmptyWhenOnlySwitchTargetsRemain(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: "/bin/engine"})
	service := NewCommandEngineServiceWithStore(store)
	service.newClient = func(config commandclient.Config) (commandEngineClient, error) {
		return &fakeCommandEngineClient{
			capabilities: compatibleCommandCapabilities(),
			plan: commandclient.CommandPlan{
				SchemaVersion: "1",
				Summary:       "Turn switch off",
				Commands: []commandclient.CommandIntent{{
					Targets: []commandclient.TargetRef{{Serial: "d0:73:d5:02:b1:20"}},
					Action:  commandclient.Action{Power: boolPtr(false)},
				}},
			},
		}, nil
	}
	preview, err := service.Interpret(context.Background(), "turn switch off", MockDeviceSnapshot())
	if err != nil {
		t.Fatalf("Interpret returned error: %v", err)
	}
	if !preview.Empty || len(preview.Commands) != 0 {
		t.Fatalf("preview = %#v, want empty unsupported command", preview)
	}
	if len(preview.SkippedTargets) != 1 || preview.SkippedTargets[0].Serial != "d0:73:d5:02:b1:20" {
		t.Fatalf("skipped = %#v", preview.SkippedTargets)
	}
}

func TestCommandEngineServiceRejectsIncompatibleCapabilities(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: "/bin/engine"})
	service := NewCommandEngineServiceWithStore(store)
	service.newClient = func(config commandclient.Config) (commandEngineClient, error) {
		caps := compatibleCommandCapabilities()
		caps.ExecutesCommands = true
		return &fakeCommandEngineClient{capabilities: caps}, nil
	}
	if _, err := service.Interpret(context.Background(), "turn ceiling on", MockDeviceSnapshot()); err == nil {
		t.Fatal("Interpret returned nil error, want capabilities error")
	}
}

func TestCommandEngineServiceUnavailableSidecar(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true})
	service := NewCommandEngineServiceWithStore(store)
	if _, err := service.Interpret(context.Background(), "turn ceiling on", MockDeviceSnapshot()); err == nil {
		t.Fatal("Interpret returned nil error, want sidecar lookup error")
	}
}

func TestCommandEngineServiceHonorsRequestCancellation(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: "/bin/engine"})
	client := &fakeCommandEngineClient{capabilities: compatibleCommandCapabilities(), blockInterpret: true}
	service := NewCommandEngineServiceWithStore(store)
	service.newClient = func(config commandclient.Config) (commandEngineClient, error) { return client, nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Interpret(ctx, "turn ceiling on", MockDeviceSnapshot()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Interpret error = %v, want context.Canceled", err)
	}
}

func TestCommandEngineServiceTranscribesThroughClient(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: "/bin/engine"})
	caps := compatibleCommandCapabilities()
	caps.Methods = append(caps.Methods, "transcribe")
	caps.Transcription = true
	caps.TranscriptionSchema = "1"
	client := &fakeCommandEngineClient{
		capabilities: caps,
		speechResult: commandclient.SpeechCommandResult{
			Transcript: commandclient.TranscribeResult{Text: "turn ceiling on", Language: "en"},
			Plan: commandclient.CommandPlan{
				SchemaVersion:     "1",
				Confidence:        0.95,
				ConfidenceResult:  commandclient.ConfidenceResult{Level: "high"},
				NeedsConfirmation: false,
				Summary:           "Turn on Ceiling",
				Commands: []commandclient.CommandIntent{{
					Targets: []commandclient.TargetRef{{Serial: "d0:73:d5:01:a2:c3", Label: "Ceiling"}},
					Action:  commandclient.Action{Power: boolPtr(true)},
				}},
			},
		},
	}
	service := NewCommandEngineServiceWithStore(store)
	service.newClient = func(config commandclient.Config) (commandEngineClient, error) { return client, nil }
	got, err := service.TranscribeAndInterpret(context.Background(), TranscribeCommandRequest{AudioPath: "/tmp/voice.wav", Language: "en"}, MockDeviceSnapshot())
	if err != nil {
		t.Fatalf("TranscribeAndInterpret returned error: %v", err)
	}
	if client.audio.AudioPath != "/tmp/voice.wav" || client.audio.Language != "en" {
		t.Fatalf("audio input = %#v", client.audio)
	}
	if got.Transcript.Text != "turn ceiling on" || got.Preview.Summary != "Turn on Ceiling" || !got.Preview.NeedsConfirmation {
		t.Fatalf("speech preview = %#v", got)
	}
}

func TestCommandEngineServiceRejectsVoiceWhenUnavailable(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: "/bin/engine"})
	service := NewCommandEngineServiceWithStore(store)
	service.newClient = func(config commandclient.Config) (commandEngineClient, error) {
		return &fakeCommandEngineClient{capabilities: compatibleCommandCapabilities()}, nil
	}
	if _, err := service.TranscribeAndInterpret(context.Background(), TranscribeCommandRequest{AudioPath: "/tmp/voice.wav"}, MockDeviceSnapshot()); err == nil {
		t.Fatal("TranscribeAndInterpret returned nil error, want unavailable voice error")
	}
}

func TestCommandEngineServiceTranscribesBase64Audio(t *testing.T) {
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: "/bin/engine"})
	caps := compatibleCommandCapabilities()
	caps.Methods = append(caps.Methods, "transcribe")
	caps.Transcription = true
	client := &fakeCommandEngineClient{
		capabilities: caps,
		speechResult: commandclient.SpeechCommandResult{
			Transcript: commandclient.TranscribeResult{Text: "turn ceiling on"},
			Plan: commandclient.CommandPlan{
				SchemaVersion:    "1",
				Confidence:       0.95,
				ConfidenceResult: commandclient.ConfidenceResult{Level: "high"},
				Summary:          "Turn on Ceiling",
				Commands: []commandclient.CommandIntent{{
					Targets: []commandclient.TargetRef{{Serial: "d0:73:d5:01:a2:c3"}},
					Action:  commandclient.Action{Power: boolPtr(true)},
				}},
			},
		},
	}
	service := NewCommandEngineServiceWithStore(store)
	service.newClient = func(config commandclient.Config) (commandEngineClient, error) { return client, nil }
	payload := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString([]byte("RIFF fake WAV"))
	if _, err := service.TranscribeAudioAndInterpret(context.Background(), TranscribeCommandAudioRequest{AudioBase64: payload}, MockDeviceSnapshot()); err != nil {
		t.Fatalf("TranscribeAudioAndInterpret returned error: %v", err)
	}
	if client.audio.AudioPath == "" {
		t.Fatal("audio path was not passed to client")
	}
	if _, err := os.Stat(client.audio.AudioPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp audio path still exists or stat failed unexpectedly: %v", err)
	}
}

func TestCommandEngineServiceIntegrationRuleOnlySidecar(t *testing.T) {
	binary := os.Getenv("HIKARI_COMMAND_ENGINE_TEST_BINARY")
	if binary == "" {
		t.Skip("set HIKARI_COMMAND_ENGINE_TEST_BINARY to run sidecar integration test")
	}
	store := &memoryCommandSettingsStore{}
	_ = store.SaveCommandEngineSettings(CommandEngineSettings{Enabled: true, EnginePath: binary})
	service := NewCommandEngineServiceWithStore(store)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	preview, err := service.Interpret(ctx, "turn ceiling on", MockDeviceSnapshot())
	if err != nil {
		t.Fatalf("Interpret returned error: %v", err)
	}
	if preview.Empty || len(preview.Commands) == 0 {
		t.Fatalf("preview = %#v, want command", preview)
	}
}

func compatibleCommandCapabilities() commandclient.Capabilities {
	return commandclient.Capabilities{
		ProtocolVersion:   commandclient.ProtocolVersion,
		CommandPlanSchema: "1",
		Methods:           []string{"health", "capabilities", "interpret"},
		ExecutesCommands:  false,
	}
}

type fakeCommandEngineClient struct {
	started            bool
	closed             bool
	capabilitiesCalled bool
	capabilities       commandclient.Capabilities
	plan               commandclient.CommandPlan
	input              commandclient.InterpretInput
	audio              commandclient.TranscribeInput
	speechResult       commandclient.SpeechCommandResult
	blockInterpret     bool
}

func (c *fakeCommandEngineClient) Start(ctx context.Context) error {
	c.started = true
	return ctx.Err()
}

func (c *fakeCommandEngineClient) Capabilities(ctx context.Context) (commandclient.Capabilities, error) {
	c.capabilitiesCalled = true
	return c.capabilities, ctx.Err()
}

func (c *fakeCommandEngineClient) Interpret(ctx context.Context, input commandclient.InterpretInput) (commandclient.CommandPlan, error) {
	c.input = input
	if c.blockInterpret {
		<-ctx.Done()
		return commandclient.CommandPlan{}, ctx.Err()
	}
	return c.plan, ctx.Err()
}

func (c *fakeCommandEngineClient) TranscribeAndInterpret(ctx context.Context, audio commandclient.TranscribeInput, snapshot commandclient.DeviceSnapshot) (commandclient.SpeechCommandResult, error) {
	c.audio = audio
	return c.speechResult, ctx.Err()
}

func (c *fakeCommandEngineClient) Close() error {
	c.closed = true
	return nil
}

func boolPtr(value bool) *bool {
	return &value
}
