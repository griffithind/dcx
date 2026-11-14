package helpers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// sanitizeNameRegexp matches characters not allowed in Docker container names
var sanitizeNameRegexp = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

// UniqueTestName generates a unique name for a test suitable for container naming.
// It uses a sanitized version of the test name plus a random suffix to ensure
// uniqueness across parallel test runs and avoid conflicts with leftover containers.
func UniqueTestName(t *testing.T) string {
	t.Helper()

	// Get base test name and sanitize it
	name := t.Name()
	// Replace path separators and other invalid chars with underscore
	name = sanitizeNameRegexp.ReplaceAllString(name, "_")
	// Limit length to leave room for random suffix (8 chars + underscore)
	if len(name) > 40 {
		name = name[:40]
	}

	// Add random suffix for uniqueness
	suffix := randomSuffix(8)
	return name + "_" + suffix
}

// randomSuffix generates a random hex string of the specified length.
func randomSuffix(length int) string {
	bytes := make([]byte, (length+1)/2)
	_, err := rand.Read(bytes)
	if err != nil {
		// Fallback to timestamp-based if crypto/rand fails
		return fmt.Sprintf("%x", os.Getpid())
	}
	return hex.EncodeToString(bytes)[:length]
}

// FixtureDir returns the path to the testdata directory.
func FixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(GetProjectRoot(t), "testdata")
}

// FeatureFixture returns the path to a feature fixture directory.
func FeatureFixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(FixtureDir(t), "features", name)
}

// CreateTempWorkspace creates a temporary workspace with a devcontainer config.
// Returns the workspace path. The environment is taken down (`dcx down`)
// when the test ends.
func CreateTempWorkspace(t *testing.T, devcontainerJSON string) string {
	t.Helper()

	tmpDir := tempWorkspace(t)

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Write devcontainer.json
	configPath := filepath.Join(devcontainerDir, "devcontainer.json")
	err = os.WriteFile(configPath, []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	return tmpDir
}

// SimpleImageConfig returns a minimal devcontainer.json for an image-based config.
// Uses a unique name based on the test name to avoid parallel test conflicts.
func SimpleImageConfig(t *testing.T, image string) string {
	t.Helper()
	return SimpleImageConfigWithName(image, UniqueTestName(t))
}

// SimpleImageConfigWithName returns a minimal devcontainer.json with a custom name.
func SimpleImageConfigWithName(image, name string) string {
	cfg := map[string]interface{}{
		"name":            name,
		"image":           image,
		"workspaceFolder": "/workspace",
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return string(data)
}

// SimpleImageConfigWithHostReqs returns a devcontainer.json with hostRequirements.
// The hostReqs parameter should be raw JSON fields, e.g., `"cpus": 10000`
func SimpleImageConfigWithHostReqs(t *testing.T, image, hostReqs string) string {
	t.Helper()
	return fmt.Sprintf(`{
	"name": %q,
	"image": %q,
	"workspaceFolder": "/workspace",
	"hostRequirements": {
		%s
	}
}`, UniqueTestName(t), image, hostReqs)
}

// CreateTempComposeWorkspace creates a temp workspace with compose files,
// taken down when the test ends.
func CreateTempComposeWorkspace(t *testing.T, devcontainerJSON, dockerComposeYAML string) string {
	t.Helper()

	tmpDir := tempWorkspace(t)

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Write devcontainer.json
	configPath := filepath.Join(devcontainerDir, "devcontainer.json")
	err = os.WriteFile(configPath, []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	// Write docker-compose.yml
	composePath := filepath.Join(devcontainerDir, "docker-compose.yml")
	err = os.WriteFile(composePath, []byte(dockerComposeYAML), 0644)
	require.NoError(t, err)

	return tmpDir
}

// tempWorkspace returns a new temporary workspace directory whose
// environment `dcx down` removes when the test ends (before the directory
// itself is removed; a workspace never brought up makes it a no-op).
func tempWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	registerWorkspace(dir)
	t.Cleanup(func() { _, _, _ = RunDCXInDir(t, dir, "down") })
	return dir
}

// CreateWorkspaceFiles creates a temporary workspace holding files (path
// relative to the workspace -> content), taken down when the test ends.
// Use it for workspaces with more than a devcontainer.json (Dockerfiles,
// compose files, features).
func CreateWorkspaceFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := tempWorkspace(t)
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	return dir
}
