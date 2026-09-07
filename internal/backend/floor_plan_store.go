package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const maxFloorPlanDocumentBytes = 10 << 20

type FloorPlanPreferencesDocument struct {
	Exists bool   `json:"exists"`
	Data   string `json:"data,omitempty"`
}

type SaveFloorPlanPreferencesRequest struct {
	Data string `json:"data"`
}

type FloorPlanStore interface {
	Load() (FloorPlanPreferencesDocument, error)
	Save(data string) error
}

type fileFloorPlanStore struct {
	mu   sync.Mutex
	path string
}

type memoryFloorPlanStore struct {
	mu       sync.Mutex
	document FloorPlanPreferencesDocument
}

type unavailableFloorPlanStore struct {
	err error
}

func NewFloorPlanStore() FloorPlanStore {
	dir, err := os.UserConfigDir()
	if err != nil {
		return &unavailableFloorPlanStore{err: fmt.Errorf("resolve user config directory: %w", err)}
	}
	if dir == "" {
		return &unavailableFloorPlanStore{err: errors.New("resolve user config directory: path is empty")}
	}
	return &fileFloorPlanStore{path: filepath.Join(dir, "hikari", "floor-plan.json")}
}

func (s *unavailableFloorPlanStore) Load() (FloorPlanPreferencesDocument, error) {
	return FloorPlanPreferencesDocument{}, s.err
}

func (s *unavailableFloorPlanStore) Save(string) error {
	return s.err
}

func (s *fileFloorPlanStore) Load() (FloorPlanPreferencesDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return FloorPlanPreferencesDocument{}, nil
	}
	if err != nil {
		return FloorPlanPreferencesDocument{}, fmt.Errorf("read floor plan preferences: %w", err)
	}
	if err := validateFloorPlanDocument(data); err != nil {
		return FloorPlanPreferencesDocument{}, fmt.Errorf("read floor plan preferences: %w", err)
	}
	return FloorPlanPreferencesDocument{Exists: true, Data: string(data)}, nil
}

func (s *fileFloorPlanStore) Save(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	raw := []byte(data)
	if err := validateFloorPlanDocument(raw); err != nil {
		return fmt.Errorf("save floor plan preferences: %w", err)
	}

	var formatted bytes.Buffer
	if err := json.Indent(&formatted, raw, "", "  "); err != nil {
		return fmt.Errorf("save floor plan preferences: format document: %w", err)
	}
	formatted.WriteByte('\n')
	if err := writeFileAtomic(s.path, formatted.Bytes(), 0o600); err != nil {
		return fmt.Errorf("save floor plan preferences: %w", err)
	}
	return nil
}

func (s *memoryFloorPlanStore) Load() (FloorPlanPreferencesDocument, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.document, nil
}

func (s *memoryFloorPlanStore) Save(data string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateFloorPlanDocument([]byte(data)); err != nil {
		return fmt.Errorf("save floor plan preferences: %w", err)
	}
	s.document = FloorPlanPreferencesDocument{Exists: true, Data: data}
	return nil
}

func validateFloorPlanDocument(data []byte) error {
	if len(data) == 0 {
		return errors.New("document is empty")
	}
	if len(data) > maxFloorPlanDocumentBytes {
		return fmt.Errorf("document exceeds %d bytes", maxFloorPlanDocumentBytes)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("invalid JSON document: %w", err)
	}
	if document == nil {
		return errors.New("document must be a JSON object")
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) (returnErr error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create floor plan directory: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".floor-plan-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary floor plan: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if err := os.Remove(temporaryPath); err != nil && !errors.Is(err, os.ErrNotExist) && returnErr == nil {
			returnErr = fmt.Errorf("remove temporary floor plan: %w", err)
		}
	}()

	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary floor plan permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary floor plan: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary floor plan: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary floor plan: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace floor plan: %w", err)
	}
	return nil
}
