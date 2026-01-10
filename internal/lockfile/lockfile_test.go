package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeFeatureID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ghcr.io/devcontainers/features/common-utils", "ghcr.io/devcontainers/features/common-utils"},
		{"GHCR.IO/DevContainers/Features/Common-Utils", "ghcr.io/devcontainers/features/common-utils"},
		{"Docker-in-Docker", "docker-in-docker"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := NormalizeFeatureID(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeFeatureID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
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
			result := GetPath(tt.configPath)
			if result != tt.expected {
				t.Errorf("GetPath(%q) = %q, want %q", tt.configPath, result, tt.expected)
			}
		})
	}
}

func TestLockfileNew(t *testing.T) {
	lf := New()
	if lf == nil {
		t.Fatal("New() returned nil")
	}
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

	// Get with different case (should work due to normalization)
	got, ok = lf.Get("GHCR.IO/DevContainers/Features/GO")
	if !ok {
		t.Fatal("Get() should find feature with different case")
	}
	if got.Version != feature.Version {
		t.Errorf("Version = %q, want %q", got.Version, feature.Version)
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
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "lockfile-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

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
	lockfilePath := GetPath(configPath)
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
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "lockfile-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create empty lockfile (init marker per spec)
	configPath := filepath.Join(tmpDir, "devcontainer.json")
	lockfilePath := GetPath(configPath)
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
