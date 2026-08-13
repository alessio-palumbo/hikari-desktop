package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type commandSettingsStore interface {
	LoadCommandEngineSettings() (CommandEngineSettings, error)
	SaveCommandEngineSettings(settings CommandEngineSettings) error
}

type fileCommandSettingsStore struct {
	path string
}

type memoryCommandSettingsStore struct {
	settings CommandEngineSettings
}

type persistedCommandSettings struct {
	Enabled    bool   `json:"enabled"`
	EnginePath string `json:"enginePath,omitempty"`
	ConfigPath string `json:"configPath,omitempty"`
}

func defaultCommandSettingsStore() commandSettingsStore {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return &memoryCommandSettingsStore{}
	}
	return fileCommandSettingsStore{path: filepath.Join(dir, "hikari", "command-settings.json")}
}

func (s fileCommandSettingsStore) LoadCommandEngineSettings() (CommandEngineSettings, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return defaultCommandEngineSettings(), nil
	}
	if err != nil {
		return CommandEngineSettings{}, fmt.Errorf("read command settings: %w", err)
	}
	var persisted persistedCommandSettings
	if err := json.Unmarshal(data, &persisted); err != nil {
		return CommandEngineSettings{}, fmt.Errorf("parse command settings: %w", err)
	}
	return commandSettingsFromPersisted(persisted), nil
}

func (s fileCommandSettingsStore) SaveCommandEngineSettings(settings CommandEngineSettings) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create command settings directory: %w", err)
	}
	data, err := json.MarshalIndent(persistedCommandSettings{
		Enabled:    settings.Enabled,
		EnginePath: strings.TrimSpace(settings.EnginePath),
		ConfigPath: strings.TrimSpace(settings.ConfigPath),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode command settings: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write command settings: %w", err)
	}
	return nil
}

func (s *memoryCommandSettingsStore) LoadCommandEngineSettings() (CommandEngineSettings, error) {
	if !s.settings.Enabled && s.settings.EnginePath == "" && s.settings.ConfigPath == "" {
		return defaultCommandEngineSettings(), nil
	}
	return s.settings, nil
}

func (s *memoryCommandSettingsStore) SaveCommandEngineSettings(settings CommandEngineSettings) error {
	s.settings = CommandEngineSettings{
		Enabled:    settings.Enabled,
		EnginePath: strings.TrimSpace(settings.EnginePath),
		ConfigPath: strings.TrimSpace(settings.ConfigPath),
	}
	return nil
}

func defaultCommandEngineSettings() CommandEngineSettings {
	enabled := strings.EqualFold(os.Getenv("HIKARI_COMMANDS_ENABLED"), "1") ||
		strings.EqualFold(os.Getenv("HIKARI_COMMANDS_ENABLED"), "true")
	if !enabled {
		_, enabled = bundledCommandEnginePath()
	}
	return CommandEngineSettings{
		Enabled:    enabled,
		EnginePath: strings.TrimSpace(os.Getenv("HIKARI_COMMAND_ENGINE_PATH")),
		ConfigPath: strings.TrimSpace(os.Getenv("HIKARI_COMMAND_ENGINE_CONFIG")),
	}
}

func commandSettingsFromPersisted(settings persistedCommandSettings) CommandEngineSettings {
	return CommandEngineSettings{
		Enabled:    settings.Enabled,
		EnginePath: strings.TrimSpace(settings.EnginePath),
		ConfigPath: strings.TrimSpace(settings.ConfigPath),
	}
}
