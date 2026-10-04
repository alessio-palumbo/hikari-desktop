package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
)

type EffectPreference struct {
	SpeedMS int            `json:"speedMs"`
	Params  map[string]any `json:"params,omitempty"`
}

type SaveDeviceEffectPreferenceRequest struct {
	Serial string       `json:"serial"`
	Effect DeviceEffect `json:"effect"`
	EffectPreference
}

type EffectSettingsStore interface {
	Load(serial string) (map[DeviceEffect]EffectPreference, error)
	Save(SaveDeviceEffectPreferenceRequest) error
}

type effectSettingsDocument struct {
	Version int                                          `json:"version"`
	Devices map[string]map[DeviceEffect]EffectPreference `json:"devices"`
}

type fileEffectSettingsStore struct {
	mu        sync.Mutex
	path      string
	pathError error
}

func NewEffectSettingsStore() EffectSettingsStore {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return &fileEffectSettingsStore{pathError: errors.New("user config directory is unavailable")}
	}
	return &fileEffectSettingsStore{path: filepath.Join(dir, "hikari", "effect-settings.json")}
}

func (s *fileEffectSettingsStore) read() (effectSettingsDocument, error) {
	document := effectSettingsDocument{Version: 1, Devices: map[string]map[DeviceEffect]EffectPreference{}}
	if s.pathError != nil {
		return document, s.pathError
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return document, nil
	}
	if err != nil {
		return document, fmt.Errorf("read effect settings: %w", err)
	}
	if len(data) > 1<<20 {
		return document, errors.New("effect settings document is too large")
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return document, fmt.Errorf("read effect settings: %w", err)
	}
	if document.Version != 1 {
		return document, errors.New("unsupported effect settings version")
	}
	if document.Devices == nil {
		document.Devices = map[string]map[DeviceEffect]EffectPreference{}
	}
	return document, nil
}

func (s *fileEffectSettingsStore) Load(serial string) (map[DeviceEffect]EffectPreference, error) {
	serial, err := effectSettingsSerial(serial)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.read()
	if err != nil {
		return nil, err
	}
	result := map[DeviceEffect]EffectPreference{}
	for effect, preference := range document.Devices[serial] {
		if validateEffectPreference(effect, preference) == nil {
			result[effect] = preference
		}
	}
	return result, nil
}

func (s *fileEffectSettingsStore) Save(req SaveDeviceEffectPreferenceRequest) error {
	serial, err := effectSettingsSerial(req.Serial)
	if err != nil {
		return err
	}
	if err := validateEffectPreference(req.Effect, req.EffectPreference); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.read()
	if err != nil {
		return err
	}
	if document.Devices[serial] == nil {
		document.Devices[serial] = map[DeviceEffect]EffectPreference{}
	}
	document.Devices[serial][req.Effect] = req.EffectPreference
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > 1<<20 {
		return errors.New("effect settings document is too large")
	}
	if err := writeFileAtomic(s.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("save effect settings: %w", err)
	}
	return nil
}

func effectSettingsSerial(serial string) (string, error) {
	id, err := parseDeviceSerial(Device{Serial: strings.TrimSpace(serial)})
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

func validateEffectPreference(effect DeviceEffect, preference EffectPreference) error {
	maximum := 30000
	switch effect {
	case DeviceEffectMove:
		maximum = 60000
	case DeviceEffectClouds:
		maximum = 100000
	case DeviceEffectFlame, DeviceEffectMorph:
		maximum = 25000
	default:
		if !isAppEffect(effect) {
			return errors.New("unknown effect")
		}
	}
	if preference.SpeedMS < 1000 || preference.SpeedMS > maximum {
		return errors.New("invalid effect speed")
	}
	if len(preference.Params) == 0 {
		return nil
	}
	definitions, err := EffectParameterDefinitions(effect)
	if err != nil {
		return err
	}
	registry, _ := lifxeffects.Definition(lifxeffects.EffectID(effect))
	for key, value := range preference.Params {
		found := false
		for _, definition := range definitions {
			if definition.Key != key {
				continue
			}
			found = true
			// Device-derived size limits are checked when loading for that device.
			if err := validateEffectParameter(definition, value, false); err != nil {
				return err
			}
			number, numeric := effectParameterNumber(value)
			for _, parameter := range registry.Params {
				if numeric && parameter.Key == key && ((parameter.Max != nil && number > *parameter.Max) || (parameter.Max == nil && number > maxEffectPreviewCells)) {
					return fmt.Errorf("invalid effect parameter %q", key)
				}
			}
		}
		if !found {
			return fmt.Errorf("unsupported effect parameter %q", key)
		}
	}
	return nil
}

// Reconcile stored preferences with the current device's supported controls.
func NormalizeEffectPreference(preference EffectPreference, parameters []EffectParameter) EffectPreference {
	sanitized := map[string]any{}
	for _, parameter := range parameters {
		value, ok := preference.Params[parameter.Key]
		if !ok {
			continue
		}
		if parameter.Kind != "choice" {
			if number, numeric := effectParameterNumber(value); numeric {
				value = max(parameter.Min, min(parameter.Max, number))
			}
		}
		sanitized[parameter.Key] = value
	}
	preference.Params = sanitized
	return preference
}
