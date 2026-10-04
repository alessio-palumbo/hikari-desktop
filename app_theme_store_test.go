package main

import (
	"errors"
	"reflect"
	"testing"

	"hikari-desktop/internal/backend"
)

type recordingThemeStore struct {
	themes  []backend.UserTheme
	saved   backend.SaveUserThemeRequest
	deleted string
	err     error
}

func (s *recordingThemeStore) Load() ([]backend.UserTheme, error) { return s.themes, s.err }
func (s *recordingThemeStore) Save(req backend.SaveUserThemeRequest) (backend.UserTheme, error) {
	s.saved = req
	return backend.UserTheme{ID: "user-example", Theme: req.Theme}, s.err
}
func (s *recordingThemeStore) Delete(id string) error { s.deleted = id; return s.err }

func TestAppUserThemesUseStorageWithoutTransport(t *testing.T) {
	store := &recordingThemeStore{themes: []backend.UserTheme{{ID: "user-example"}}}
	// No transport is needed: save/rename/delete must never control devices.
	app := &App{themes: store}
	loaded, err := app.GetUserThemes()
	if err != nil || !reflect.DeepEqual(loaded, store.themes) {
		t.Fatalf("load: %#v %v", loaded, err)
	}
	req := backend.SaveUserThemeRequest{ID: "user-example"}
	entry, err := app.SaveUserTheme(req)
	if err != nil || entry.ID != req.ID || !reflect.DeepEqual(store.saved, req) {
		t.Fatalf("save: %#v %v", entry, err)
	}
	if err := app.DeleteUserTheme(req.ID); err != nil || store.deleted != req.ID {
		t.Fatalf("delete: %v", err)
	}
	store.err = errors.New("storage unavailable")
	if _, err := app.GetUserThemes(); !errors.Is(err, store.err) {
		t.Fatal("load error lost")
	}
	if _, err := app.SaveUserTheme(req); !errors.Is(err, store.err) {
		t.Fatal("save error lost")
	}
	if err := app.DeleteUserTheme(req.ID); !errors.Is(err, store.err) {
		t.Fatal("delete error lost")
	}
}
