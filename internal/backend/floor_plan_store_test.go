package backend

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFileFloorPlanStoreReturnsMissingDocument(t *testing.T) {
	store := &fileFloorPlanStore{path: filepath.Join(t.TempDir(), "floor-plan.json")}

	document, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if document.Exists || document.Data != "" {
		t.Fatalf("Load returned %#v", document)
	}
}

func TestFileFloorPlanStoreSavesAndLoadsJSONAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hikari", "floor-plan.json")
	store := &fileFloorPlanStore{path: path}
	input := `{"version":2,"profiles":{"home":{"name":"Home"}}}`

	if err := store.Save(input); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	document, err := store.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !document.Exists || !strings.Contains(document.Data, `"version": 2`) || !strings.Contains(document.Data, `"Home"`) {
		t.Fatalf("Load returned %#v", document)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".floor-plan-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain: %#v", matches)
	}
}

func TestFileFloorPlanStoreRejectsInvalidJSONWithoutReplacingExistingData(t *testing.T) {
	store := &fileFloorPlanStore{path: filepath.Join(t.TempDir(), "floor-plan.json")}
	if err := store.Save(`{"version":2,"profiles":{}}`); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(`{"version":`); err == nil {
		t.Fatal("Save returned nil error for invalid JSON")
	}
	document, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !document.Exists || !strings.Contains(document.Data, `"version": 2`) {
		t.Fatalf("existing document was not preserved: %#v", document)
	}
}

func TestMemoryFloorPlanStoreValidatesDocuments(t *testing.T) {
	store := &memoryFloorPlanStore{}
	if err := store.Save(`[]`); err == nil {
		t.Fatal("Save returned nil error for a non-object document")
	}
	if err := store.Save(`{"version":2}`); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	document, err := store.Load()
	if err != nil || !document.Exists || document.Data != `{"version":2}` {
		t.Fatalf("Load returned %#v, %v", document, err)
	}
}
