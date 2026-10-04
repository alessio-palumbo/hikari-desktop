package backend

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	lifxeffects "github.com/alessio-palumbo/lifxlan-go/pkg/effects"
	lifxthemes "github.com/alessio-palumbo/lifxlan-go/pkg/themes"
)

func storedTestTheme(name string) lifxthemes.Theme {
	return lifxthemes.Theme{Name: name, Palette: lifxeffects.Palette{Base: []lifxeffects.Color{{Hue: 210, Saturation: 80, Brightness: 100, Kelvin: 3500}}}, Layout: lifxthemes.Gradient, Axis: lifxthemes.Horizontal}
}

func TestUserThemesPersistCreateRenameDuplicateAndDelete(t *testing.T) {
	store := &fileThemeStore{path: filepath.Join(t.TempDir(), "hikari", "themes.json")}
	if themes, err := store.Load(); err != nil || themes == nil || len(themes) != 0 {
		t.Fatalf("empty: %#v %v", themes, err)
	}
	first, err := store.Save(SaveUserThemeRequest{Theme: storedTestTheme("  Ocean  ")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Save(SaveUserThemeRequest{Theme: first.Theme})
	if err != nil || first.ID == second.ID || first.Theme.Name != "Ocean" {
		t.Fatalf("duplicate: %#v %v", second, err)
	}
	first.Theme = storedTestTheme("Amber")
	first.Theme.Palette.Base[0].Hue = 25
	updated, err := store.Save(SaveUserThemeRequest{ID: first.ID, Theme: first.Theme})
	if err != nil || updated.ID != first.ID {
		t.Fatalf("update: %#v %v", updated, err)
	}
	restarted := &fileThemeStore{path: store.path}
	loaded, err := restarted.Load()
	if err != nil || !reflect.DeepEqual(loaded, []UserTheme{updated, second}) {
		t.Fatalf("restart: %#v %v", loaded, err)
	}
	loaded[0].Theme.Palette.Base[0].Hue = 300
	again, _ := restarted.Load()
	if again[0].Theme.Palette.Base[0].Hue != 25 {
		t.Fatal("caller mutated stored state")
	}
	if err := restarted.Delete(first.ID); err != nil {
		t.Fatal(err)
	}
	remaining, err := restarted.Load()
	if err != nil || !reflect.DeepEqual(remaining, []UserTheme{second}) {
		t.Fatalf("delete: %#v %v", remaining, err)
	}
	if err := restarted.Delete(second.ID); err != nil {
		t.Fatal(err)
	}
	empty, err := restarted.Load()
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("last delete: %#v %v", empty, err)
	}
	info, _ := os.Stat(store.path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions: %o", info.Mode().Perm())
	}
	entries, _ := os.ReadDir(filepath.Dir(store.path))
	if len(entries) != 1 {
		t.Fatal("temporary files left behind")
	}
}

func TestUserThemesRejectInvalidWritesWithoutChangingFile(t *testing.T) {
	store := &fileThemeStore{path: filepath.Join(t.TempDir(), "themes.json")}
	valid := storedTestTheme("Ocean")
	if _, err := store.Save(SaveUserThemeRequest{Theme: valid}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(store.path)
	for _, change := range []func(*lifxthemes.Theme){
		func(theme *lifxthemes.Theme) { theme.Name = " " },
		func(theme *lifxthemes.Theme) { theme.Name = strings.Repeat("a", 129) },
		func(theme *lifxthemes.Theme) { theme.Layout = "unknown" },
		func(theme *lifxthemes.Theme) { theme.Axis = "unknown" },
		func(theme *lifxthemes.Theme) { theme.Palette.Base = nil },
		func(theme *lifxthemes.Theme) { theme.Palette.Base[0].Hue = math.NaN() },
		func(theme *lifxthemes.Theme) { theme.Palette.Base[0].Kelvin = 0 },
		func(theme *lifxthemes.Theme) { theme.Palette.Base = make([]lifxeffects.Color, 65) },
	} {
		theme := storedTestTheme("Ocean")
		change(&theme)
		if _, err := store.Save(SaveUserThemeRequest{Theme: theme}); err == nil {
			t.Fatalf("invalid accepted: %#v", theme)
		}
	}
	for _, id := range []string{"builtin-ocean", "../themes", "user-missing"} {
		if _, err := store.Save(SaveUserThemeRequest{ID: id, Theme: valid}); err == nil {
			t.Fatalf("update accepted %q", id)
		}
		if err := store.Delete(id); err == nil {
			t.Fatalf("delete accepted %q", id)
		}
	}
	after, _ := os.ReadFile(store.path)
	if string(before) != string(after) {
		t.Fatal("invalid write changed stored themes")
	}
}

func TestUserThemesDoNotOverwriteCorruptOrFutureDocuments(t *testing.T) {
	entry := UserTheme{ID: "user-example", Theme: storedTestTheme("Ocean")}
	duplicate, _ := json.Marshal(themeDocument{Version: 1, Themes: []UserTheme{entry, entry}})
	for _, data := range []string{"broken", "null", "{}", `{"version":2,"themes":[]}`, string(duplicate), strings.Repeat(" ", maxThemeDocumentBytes+1)} {
		store := &fileThemeStore{path: filepath.Join(t.TempDir(), "themes.json")}
		if err := os.WriteFile(store.path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Load(); err == nil {
			t.Fatal("invalid document accepted")
		}
		if _, err := store.Save(SaveUserThemeRequest{Theme: storedTestTheme("New")}); err == nil {
			t.Fatal("invalid document overwritten")
		}
		if err := store.Delete(entry.ID); err == nil {
			t.Fatal("invalid document modified on deletion")
		}
		after, _ := os.ReadFile(store.path)
		if string(after) != data {
			t.Fatal("original data lost")
		}
	}
}

func TestUserThemesConcurrentCreatesAndDeterministicSorting(t *testing.T) {
	store := &fileThemeStore{path: filepath.Join(t.TempDir(), "themes.json")}
	var wait sync.WaitGroup
	for _, name := range []string{"Ocean", "amber", "Amber", "Zen"} {
		wait.Add(1)
		go func(name string) {
			defer wait.Done()
			if _, err := store.Save(SaveUserThemeRequest{Theme: storedTestTheme(name)}); err != nil {
				t.Error(err)
			}
		}(name)
	}
	wait.Wait()
	first, err := store.Load()
	second, _ := store.Load()
	if err != nil || len(first) != 4 || !reflect.DeepEqual(first, second) {
		t.Fatalf("concurrent saves: %#v %v", first, err)
	}
	if strings.ToLower(first[0].Theme.Name) != "amber" || first[2].Theme.Name != "Ocean" || first[3].Theme.Name != "Zen" {
		t.Fatal("unexpected order")
	}
}
