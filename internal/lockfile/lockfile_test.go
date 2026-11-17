package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCompareIDs pins the localeCompare order devcontainers/cli writes
// lockfile keys in, where it differs from byte order.
func TestCompareIDs(t *testing.T) {
	ordered := []string{
		"a",
		"a_1", // a prefix first; punctuation '_' < '-' < '/' before digits
		"a-b",
		"a/b",
		"a1",
		"ab",
		"aB", // case only breaks ties, lowercase first
		"B",  // letters ignore case: 'a' < 'B' < 'c'
		"c",
	}
	for i := range ordered {
		for j := range ordered {
			got := CompareIDs(ordered[i], ordered[j])
			switch {
			case i < j && got >= 0, i > j && got <= 0, i == j && got != 0:
				t.Errorf("CompareIDs(%q, %q) = %d", ordered[i], ordered[j], got)
			}
		}
	}
}

// TestSaveWritesDevcontainersCLIForm: the same bytes JSON.stringify(lockfile,
// null, 2) + "\n" gives, so the two tools never churn the file.
func TestSaveWritesDevcontainersCLIForm(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "devcontainer.json")
	lf := New()
	lf.Set("ghcr.io/devcontainers/features/node:1", LockedFeature{
		Version: "1.6.1", Resolved: "ghcr.io/devcontainers/features/node@sha256:aa", Integrity: "sha256:aa",
	})
	lf.Set("ghcr.io/devcontainers-extra/features/tool<x>:1", LockedFeature{
		Version: "2.0.0", Resolved: "ghcr.io/devcontainers-extra/features/tool@sha256:bb", Integrity: "sha256:bb",
		DependsOn: []string{"ghcr.io/devcontainers/features/node:1", "ghcr.io/devcontainers/features/common-utils"},
	})
	if err := lf.Save(configPath); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(Path(configPath))
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "features": {
    "ghcr.io/devcontainers-extra/features/tool<x>:1": {
      "version": "2.0.0",
      "resolved": "ghcr.io/devcontainers-extra/features/tool@sha256:bb",
      "integrity": "sha256:bb",
      "dependsOn": [
        "ghcr.io/devcontainers/features/node:1",
        "ghcr.io/devcontainers/features/common-utils"
      ]
    },
    "ghcr.io/devcontainers/features/node:1": {
      "version": "1.6.1",
      "resolved": "ghcr.io/devcontainers/features/node@sha256:aa",
      "integrity": "sha256:aa"
    }
  }
}
`
	if string(data) != want {
		t.Errorf("lockfile:\n%s\nwant:\n%s", data, want)
	}

	if err := New().Save(configPath); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(Path(configPath))
	if string(data) != "{\n  \"features\": {}\n}\n" {
		t.Errorf("empty lockfile: %q", data)
	}
}

func TestGetPath(t *testing.T) {
	tests := []struct {
		configPath string
		expected   string
	}{
		{"/workspace/.devcontainer/devcontainer.json", "/workspace/.devcontainer/devcontainer-lock.json"},
		{"/workspace/.devcontainer.json", "/workspace/.devcontainer-lock.json"},
		{"/workspace/devcontainer.json", "/workspace/devcontainer-lock.json"},
	}

	for _, tt := range tests {
		t.Run(tt.configPath, func(t *testing.T) {
			result := Path(tt.configPath)
			if result != tt.expected {
				t.Errorf("Path(%q) = %q, want %q", tt.configPath, result, tt.expected)
			}
		})
	}
}

func TestLockfileNew(t *testing.T) {
	lf := New()
	// New always returns a non-nil *Lockfile with an initialized map.
	if lf.Features == nil {
		t.Error("Features map is nil")
	}
	if len(lf.Features) != 0 {
		t.Error("Features map is not empty")
	}
}

func TestLockfileSetGet(t *testing.T) {
	lf := New()

	feature := LockedFeature{
		Version:   "1.2.3",
		Resolved:  "ghcr.io/devcontainers/features/go@sha256:abc123",
		Integrity: "sha256:def456",
		DependsOn: []string{"common-utils"},
	}

	lf.Set("ghcr.io/devcontainers/features/go", feature)

	// Get with exact ID
	got, ok := lf.Get("ghcr.io/devcontainers/features/go")
	if !ok {
		t.Fatal("Get() returned false for existing feature")
	}
	if got.Version != feature.Version {
		t.Errorf("Version = %q, want %q", got.Version, feature.Version)
	}
	if got.Resolved != feature.Resolved {
		t.Errorf("Resolved = %q, want %q", got.Resolved, feature.Resolved)
	}
	if got.Integrity != feature.Integrity {
		t.Errorf("Integrity = %q, want %q", got.Integrity, feature.Integrity)
	}

	// Keys are the references as written (devcontainers/cli looks them
	// up exactly).
	if _, ok = lf.Get("GHCR.IO/DevContainers/Features/GO"); ok {
		t.Error("Get() matched a differently cased reference")
	}

	// Get non-existent feature
	_, ok = lf.Get("non-existent")
	if ok {
		t.Error("Get() returned true for non-existent feature")
	}
}

func TestLockfileIsEmpty(t *testing.T) {
	// Nil lockfile
	var lf *Lockfile
	if !lf.IsEmpty() {
		t.Error("nil lockfile should be empty")
	}

	// New empty lockfile
	lf = New()
	if !lf.IsEmpty() {
		t.Error("new lockfile should be empty")
	}

	// Lockfile with feature
	lf.Set("test", LockedFeature{Version: "1.0.0"})
	if lf.IsEmpty() {
		t.Error("lockfile with feature should not be empty")
	}
}

func TestLockfileEquals(t *testing.T) {
	// Both nil
	var lf1, lf2 *Lockfile
	if !lf1.Equals(lf2) {
		t.Error("two nil lockfiles should be equal")
	}

	// One nil
	lf1 = New()
	if lf1.Equals(lf2) {
		t.Error("nil and non-nil lockfiles should not be equal")
	}

	// Both empty
	lf2 = New()
	if !lf1.Equals(lf2) {
		t.Error("two empty lockfiles should be equal")
	}

	// Same content
	feature := LockedFeature{
		Version:   "1.0.0",
		Resolved:  "test@sha256:abc",
		Integrity: "sha256:xyz",
		DependsOn: []string{"dep1"},
	}
	lf1.Set("test", feature)
	lf2.Set("test", feature)
	if !lf1.Equals(lf2) {
		t.Error("lockfiles with same content should be equal")
	}

	// Different content
	lf2.Set("test", LockedFeature{Version: "2.0.0"})
	if lf1.Equals(lf2) {
		t.Error("lockfiles with different content should not be equal")
	}
}

func TestLockfileSaveLoad(t *testing.T) {
	// Create temp directory (automatically cleaned up)
	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, "devcontainer.json")

	// Create lockfile with content
	lf := New()
	lf.Set("ghcr.io/devcontainers/features/go", LockedFeature{
		Version:   "1.2.3",
		Resolved:  "ghcr.io/devcontainers/features/go@sha256:abc123",
		Integrity: "sha256:def456",
		DependsOn: []string{"common-utils"},
	})

	// Save
	if err := lf.Save(configPath); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify file exists
	lockfilePath := Path(configPath)
	if _, err := os.Stat(lockfilePath); err != nil {
		t.Fatalf("lockfile not created: %v", err)
	}

	// Load
	loaded, initMarker, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if initMarker {
		t.Error("initMarker should be false for non-empty file")
	}
	if loaded == nil {
		t.Fatal("Load() returned nil")
	}

	// Verify content
	if !lf.Equals(loaded) {
		t.Error("loaded lockfile doesn't match saved lockfile")
	}
}

func TestLockfileLoadNonExistent(t *testing.T) {
	lf, initMarker, err := Load("/non/existent/path/devcontainer.json")
	if err != nil {
		t.Errorf("Load() should not error for non-existent file: %v", err)
	}
	if initMarker {
		t.Error("initMarker should be false for non-existent file")
	}
	if lf != nil {
		t.Error("Load() should return nil for non-existent file")
	}
}

func TestLockfileLoadEmptyFile(t *testing.T) {
	// Create temp directory (automatically cleaned up)
	tmpDir := t.TempDir()

	// Create empty lockfile (init marker per spec)
	configPath := filepath.Join(tmpDir, "devcontainer.json")
	lockfilePath := Path(configPath)
	if err := os.WriteFile(lockfilePath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	lf, initMarker, err := Load(configPath)
	if err != nil {
		t.Errorf("Load() error: %v", err)
	}
	if !initMarker {
		t.Error("initMarker should be true for empty file")
	}
	if lf != nil {
		t.Error("Load() should return nil for empty file")
	}
}
