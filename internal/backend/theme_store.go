package backend

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	lifxthemes "github.com/alessio-palumbo/lifxlan-go/pkg/themes"
)

const maxThemeDocumentBytes = 1 << 20

// UserTheme stores only palette/layout data, never targets or device state.
type UserTheme struct {
	ID    string           `json:"id"`
	Theme lifxthemes.Theme `json:"theme"`
}

type SaveUserThemeRequest struct {
	// Empty creates a new theme; an existing ID updates that theme.
	ID    string           `json:"id,omitempty"`
	Theme lifxthemes.Theme `json:"theme"`
}

type ThemeStore interface {
	Load() ([]UserTheme, error)
	Save(SaveUserThemeRequest) (UserTheme, error)
	Delete(id string) error
}

type themeDocument struct {
	Version int         `json:"version"`
	Themes  []UserTheme `json:"themes"`
}

type fileThemeStore struct {
	mu        sync.Mutex
	path      string
	pathError error
}

func NewThemeStore() ThemeStore {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return &fileThemeStore{pathError: errors.New("user config directory is unavailable")}
	}
	return &fileThemeStore{path: filepath.Join(dir, "hikari", "themes.json")}
}

func (s *fileThemeStore) read() (themeDocument, error) {
	document := themeDocument{Version: 1, Themes: []UserTheme{}}
	if s.pathError != nil {
		return document, s.pathError
	}
	file, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return document, nil
	}
	if err != nil {
		return document, fmt.Errorf("read user themes: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxThemeDocumentBytes+1))
	if err != nil {
		return document, fmt.Errorf("read user themes: %w", err)
	}
	if len(data) > maxThemeDocumentBytes {
		return document, errors.New("user themes document is too large")
	}
	document = themeDocument{}
	if err := json.Unmarshal(data, &document); err != nil {
		return document, fmt.Errorf("read user themes: %w", err)
	}
	if document.Version != 1 {
		return document, errors.New("unsupported user themes version")
	}
	if len(document.Themes) > 256 {
		return document, errors.New("too many user themes")
	}
	seen := map[string]bool{}
	for _, entry := range document.Themes {
		if !validUserThemeID(entry.ID) || seen[entry.ID] {
			return document, errors.New("invalid or duplicate user theme ID")
		}
		seen[entry.ID] = true
		if err := validateUserTheme(entry.Theme); err != nil {
			return document, fmt.Errorf("read user theme %s: %w", entry.ID, err)
		}
	}
	if document.Themes == nil {
		document.Themes = []UserTheme{}
	}
	return document, nil
}

func (s *fileThemeStore) Load() ([]UserTheme, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.read()
	if err != nil {
		return nil, err
	}
	slices.SortFunc(document.Themes, func(a, b UserTheme) int {
		if order := strings.Compare(strings.ToLower(a.Theme.Name), strings.ToLower(b.Theme.Name)); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return document.Themes, nil
}

func (s *fileThemeStore) Save(req SaveUserThemeRequest) (UserTheme, error) {
	req.Theme.Name = strings.TrimSpace(req.Theme.Name)
	if err := validateUserTheme(req.Theme); err != nil {
		return UserTheme{}, err
	}
	if req.ID != "" && !validUserThemeID(req.ID) {
		return UserTheme{}, errors.New("invalid user theme ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.read()
	if err != nil {
		return UserTheme{}, err
	}
	entry := UserTheme{ID: req.ID, Theme: req.Theme}
	if entry.ID == "" {
		if len(document.Themes) >= 256 {
			return UserTheme{}, errors.New("too many user themes")
		}
		entry.ID = "user-" + rand.Text()
		document.Themes = append(document.Themes, entry)
	} else {
		index := slices.IndexFunc(document.Themes, func(theme UserTheme) bool { return theme.ID == entry.ID })
		if index < 0 {
			return UserTheme{}, errors.New("user theme no longer exists")
		}
		document.Themes[index] = entry
	}
	if err := s.write(document); err != nil {
		return UserTheme{}, err
	}
	return entry, nil
}

func (s *fileThemeStore) Delete(id string) error {
	if !validUserThemeID(id) {
		return errors.New("invalid user theme ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	document, err := s.read()
	if err != nil {
		return err
	}
	index := slices.IndexFunc(document.Themes, func(theme UserTheme) bool { return theme.ID == id })
	if index < 0 {
		return errors.New("user theme no longer exists")
	}
	document.Themes = slices.Delete(document.Themes, index, index+1)
	return s.write(document)
}

func (s *fileThemeStore) write(document themeDocument) error {
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if len(data)+1 > maxThemeDocumentBytes {
		return errors.New("user themes document is too large")
	}
	if err := writeFileAtomic(s.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("save user themes: %w", err)
	}
	return nil
}

func validateUserTheme(theme lifxthemes.Theme) error {
	if !utf8.ValidString(theme.Name) || utf8.RuneCountInString(theme.Name) > 128 {
		return errors.New("theme name must contain at most 128 characters")
	}
	if len(theme.Palette.Base)+len(theme.Palette.Accents)+len(theme.Palette.Backgrounds) > 64 {
		return errors.New("theme palette must contain at most 64 colors")
	}
	return theme.Validate()
}

func validUserThemeID(id string) bool {
	if !strings.HasPrefix(id, "user-") || len(id) <= 5 || len(id) > 80 {
		return false
	}
	for _, char := range id[5:] {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-') {
			return false
		}
	}
	return true
}
