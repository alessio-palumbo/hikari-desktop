package backend

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	commandclient "github.com/alessio-palumbo/lifx-command-engine/client"
)

const (
	commandEnginePlanSchema     = "1"
	commandEngineStartupTimeout = 5 * time.Second
	commandEngineRequestTimeout = 8 * time.Second
)

type commandEngineClient interface {
	Start(ctx context.Context) error
	Capabilities(ctx context.Context) (commandclient.Capabilities, error)
	Interpret(ctx context.Context, input commandclient.InterpretInput) (commandclient.CommandPlan, error)
	Close() error
}

type commandEngineClientFactory func(commandclient.Config) (commandEngineClient, error)

type CommandEngineService struct {
	store     commandSettingsStore
	newClient commandEngineClientFactory
	stderr    io.Writer

	mu       sync.Mutex
	client   commandEngineClient
	settings CommandEngineSettings
	started  bool
}

func NewCommandEngineService() *CommandEngineService {
	return NewCommandEngineServiceWithStore(defaultCommandSettingsStore())
}

func NewCommandEngineServiceWithStore(store commandSettingsStore) *CommandEngineService {
	return &CommandEngineService{
		store:     store,
		newClient: newCommandEngineClient,
		stderr:    commandEngineLogWriter{},
	}
}

func newCommandEngineClient(config commandclient.Config) (commandEngineClient, error) {
	return commandclient.New(config)
}

func (s *CommandEngineService) Settings(ctx context.Context) (CommandEngineSettings, error) {
	settings, err := s.loadSettings()
	if err != nil {
		return CommandEngineSettings{}, err
	}
	return s.decorateSettings(settings), nil
}

func (s *CommandEngineService) SetSettings(ctx context.Context, req SetCommandEngineSettingsRequest) (CommandEngineSettings, error) {
	settings := CommandEngineSettings{
		Enabled:    req.Enabled,
		EnginePath: strings.TrimSpace(req.EnginePath),
		ConfigPath: strings.TrimSpace(req.ConfigPath),
	}
	if err := s.store.SaveCommandEngineSettings(settings); err != nil {
		return CommandEngineSettings{}, err
	}
	s.mu.Lock()
	if s.client != nil {
		_ = s.client.Close()
	}
	s.client = nil
	s.started = false
	s.settings = settings
	s.mu.Unlock()
	return s.decorateSettings(settings), nil
}

func (s *CommandEngineService) Interpret(ctx context.Context, text string, snapshot DeviceSnapshot) (CommandPreview, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return CommandPreview{}, fmt.Errorf("command text is required")
	}
	settings, err := s.loadSettings()
	if err != nil {
		return CommandPreview{}, err
	}
	if !settings.Enabled {
		return CommandPreview{}, fmt.Errorf("local text commands are disabled")
	}
	client, err := s.ensureStarted(ctx, settings)
	if err != nil {
		return CommandPreview{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, commandEngineRequestTimeout)
	defer cancel()
	plan, err := client.Interpret(requestCtx, commandclient.InterpretInput{
		Text:     text,
		Snapshot: CommandSnapshotFromDeviceSnapshot(snapshot),
	})
	if err != nil {
		return CommandPreview{}, fmt.Errorf("interpret command: %w", err)
	}
	preview, err := commandPreviewFromPlan(plan, snapshot)
	if err != nil {
		return CommandPreview{}, err
	}
	return preview, nil
}

func (s *CommandEngineService) Close(ctx context.Context) error {
	s.mu.Lock()
	client := s.client
	s.client = nil
	s.started = false
	s.mu.Unlock()
	if client == nil {
		return nil
	}
	return client.Close()
}

func (s *CommandEngineService) ensureStarted(ctx context.Context, settings CommandEngineSettings) (commandEngineClient, error) {
	path, args, err := commandEngineCommand(settings)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil && s.started && sameCommandSettings(s.settings, settings) {
		return s.client, nil
	}
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
		s.started = false
	}
	client, err := s.newClient(commandclient.Config{
		Path:           path,
		Args:           args,
		RestartOnCrash: true,
		StartupTimeout: commandEngineStartupTimeout,
		Stderr:         s.stderr,
	})
	if err != nil {
		return nil, err
	}
	startCtx, cancel := context.WithTimeout(ctx, commandEngineStartupTimeout+time.Second)
	defer cancel()
	if err := client.Start(startCtx); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("start command engine: %w", err)
	}
	capabilities, err := client.Capabilities(startCtx)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("read command engine capabilities: %w", err)
	}
	if err := validateCommandEngineCapabilities(capabilities); err != nil {
		_ = client.Close()
		return nil, err
	}
	s.client = client
	s.settings = settings
	s.started = true
	return client, nil
}

func (s *CommandEngineService) loadSettings() (CommandEngineSettings, error) {
	settings, err := s.store.LoadCommandEngineSettings()
	if err != nil {
		return CommandEngineSettings{}, err
	}
	if settings.EnginePath == "" {
		settings.EnginePath = strings.TrimSpace(os.Getenv("HIKARI_COMMAND_ENGINE_PATH"))
	}
	if settings.ConfigPath == "" {
		settings.ConfigPath = strings.TrimSpace(os.Getenv("HIKARI_COMMAND_ENGINE_CONFIG"))
	}
	return settings, nil
}

func (s *CommandEngineService) decorateSettings(settings CommandEngineSettings) CommandEngineSettings {
	if !settings.Enabled {
		return settings
	}
	if _, _, err := commandEngineCommand(settings); err != nil {
		settings.Warning = err.Error()
		settings.Available = false
		return settings
	}
	settings.Available = true
	return settings
}

func commandEngineCommand(settings CommandEngineSettings) (string, []string, error) {
	path := strings.TrimSpace(settings.EnginePath)
	if path == "" {
		if resolved, ok := bundledCommandEnginePath(); ok {
			path = resolved
		} else if resolved, err := exec.LookPath("lifx-command-engine"); err == nil {
			path = resolved
		}
	}
	if path == "" {
		return "", nil, fmt.Errorf("lifx-command-engine binary not found; set a command engine path")
	}
	args := []string{}
	if strings.TrimSpace(settings.ConfigPath) != "" {
		args = append(args, "serve", "-config", strings.TrimSpace(settings.ConfigPath))
	}
	return path, args, nil
}

func bundledCommandEnginePath() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, "lifx-command-engine"),
		filepath.Clean(filepath.Join(exeDir, "..", "Resources", "lifx-command-engine")),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, true
		}
	}
	return "", false
}

func validateCommandEngineCapabilities(capabilities commandclient.Capabilities) error {
	if capabilities.ProtocolVersion != commandclient.ProtocolVersion {
		return fmt.Errorf("unsupported command engine protocol %q", capabilities.ProtocolVersion)
	}
	if capabilities.CommandPlanSchema != commandEnginePlanSchema {
		return fmt.Errorf("unsupported command plan schema %q", capabilities.CommandPlanSchema)
	}
	if capabilities.ExecutesCommands {
		return fmt.Errorf("command engine must not execute commands")
	}
	if !containsString(capabilities.Methods, "interpret") {
		return fmt.Errorf("command engine does not support interpret")
	}
	return nil
}

func sameCommandSettings(a, b CommandEngineSettings) bool {
	return a.Enabled == b.Enabled &&
		strings.TrimSpace(a.EnginePath) == strings.TrimSpace(b.EnginePath) &&
		strings.TrimSpace(a.ConfigPath) == strings.TrimSpace(b.ConfigPath)
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

type commandEngineLogWriter struct{}

func (commandEngineLogWriter) Write(p []byte) (int, error) {
	log.Printf("hikari command engine: %s", strings.TrimSpace(string(p)))
	return len(p), nil
}
