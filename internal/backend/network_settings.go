package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type networkSettingsStore interface {
	LoadNetworkInterfaceName() (string, error)
	SaveNetworkInterfaceName(name string) error
}

type fileNetworkSettingsStore struct {
	path string
}

type memoryNetworkSettingsStore struct {
	interfaceName string
}

type persistedNetworkSettings struct {
	SelectedInterfaceName string `json:"selectedInterfaceName"`
}

func defaultNetworkSettingsStore() networkSettingsStore {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return &memoryNetworkSettingsStore{}
	}
	return fileNetworkSettingsStore{path: filepath.Join(dir, "hikari", "settings.json")}
}

func (s fileNetworkSettingsStore) LoadNetworkInterfaceName() (string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read network settings: %w", err)
	}
	var settings persistedNetworkSettings
	if err := json.Unmarshal(data, &settings); err != nil {
		return "", fmt.Errorf("parse network settings: %w", err)
	}
	return settings.SelectedInterfaceName, nil
}

func (s fileNetworkSettingsStore) SaveNetworkInterfaceName(name string) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	data, err := json.MarshalIndent(persistedNetworkSettings{SelectedInterfaceName: name}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode network settings: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0o600); err != nil {
		return fmt.Errorf("write network settings: %w", err)
	}
	return nil
}

func (s *memoryNetworkSettingsStore) LoadNetworkInterfaceName() (string, error) {
	return s.interfaceName, nil
}

func (s *memoryNetworkSettingsStore) SaveNetworkInterfaceName(name string) error {
	s.interfaceName = name
	return nil
}
