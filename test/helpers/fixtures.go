package helpers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/config"
	"github.com/stretchr/testify/require"
)

// sanitizeNameRegexp matches characters not allowed in Docker container names
var sanitizeNameRegexp = regexp.MustCompile(`[^a-zA-Z0-9_.-]`)

// UniqueTestName generates a unique name for a test suitable for container naming.
// It uses a sanitized version of the test name, which is unique for each test.
func UniqueTestName(t *testing.T) string {
	t.Helper()

	// Get base test name and sanitize it
	name := t.Name()
	// Replace path separators and other invalid chars with underscore
	name = sanitizeNameRegexp.ReplaceAllString(name, "_")
	// Limit length to avoid overly long container names
	if len(name) > 50 {
		name = name[:50]
	}

	return name
}

// FixtureDir returns the path to the testdata directory.
func FixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(GetProjectRoot(t), "testdata")
}

// ConfigFixture returns the path to a config fixture directory.
func ConfigFixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(FixtureDir(t), "configs", name)
}

// ComposeFixture returns the path to a compose fixture directory.
func ComposeFixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(FixtureDir(t), "compose", name)
}

// FeatureFixture returns the path to a feature fixture directory.
func FeatureFixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(FixtureDir(t), "features", name)
}

// LoadTestConfig loads a devcontainer configuration from a fixture.
func LoadTestConfig(t *testing.T, fixtureName string) *config.DevContainerConfig {
	t.Helper()

	fixtureDir := ConfigFixture(t, fixtureName)
	cfg, _, err := config.Load(fixtureDir, "")
	require.NoError(t, err, "failed to load fixture config: %s", fixtureName)

	return cfg
}

// CreateTempWorkspace creates a temporary workspace with a devcontainer config.
// Returns the workspace path.
func CreateTempWorkspace(t *testing.T, devcontainerJSON string) string {
	t.Helper()

	// Create temp directory
	tmpDir := t.TempDir()

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

// CreateTempWorkspaceFromFixture creates a temp workspace by copying a fixture.
func CreateTempWorkspaceFromFixture(t *testing.T, fixtureName string) string {
	t.Helper()

	srcDir := ConfigFixture(t, fixtureName)
	tmpDir := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Copy all files from fixture
	entries, err := os.ReadDir(srcDir)
	require.NoError(t, err)

	for _, entry := range entries {
		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(devcontainerDir, entry.Name())

		data, err := os.ReadFile(srcPath)
		require.NoError(t, err)

		err = os.WriteFile(dstPath, data, 0644)
		require.NoError(t, err)
	}

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

// SimpleComposeConfig returns a devcontainer.json for a compose-based config.
// Uses a unique name based on the test name to avoid parallel test conflicts.
func SimpleComposeConfig(t *testing.T, composeFile, service string) string {
	t.Helper()
	return SimpleComposeConfigWithName(composeFile, service, UniqueTestName(t))
}

// SimpleComposeConfigWithName returns a devcontainer.json for a compose-based config with a custom name.
func SimpleComposeConfigWithName(composeFile, service, name string) string {
	cfg := map[string]interface{}{
		"name":              name,
		"dockerComposeFile": composeFile,
		"service":           service,
		"workspaceFolder":   "/workspace",
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return string(data)
}

// ConfigWithFeatures returns a devcontainer.json with features.
// Uses a unique name based on the test name to avoid parallel test conflicts.
func ConfigWithFeatures(t *testing.T, image string, features map[string]interface{}) string {
	t.Helper()
	return ConfigWithFeaturesAndName(image, features, UniqueTestName(t))
}

// ConfigWithFeaturesAndName returns a devcontainer.json with features and a custom name.
func ConfigWithFeaturesAndName(image string, features map[string]interface{}, name string) string {
	cfg := map[string]interface{}{
		"name":            name,
		"image":           image,
		"workspaceFolder": "/workspace",
		"features":        features,
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return string(data)
}

// CreateTempComposeWorkspace creates a temp workspace with compose files.
func CreateTempComposeWorkspace(t *testing.T, devcontainerJSON, dockerComposeYAML string) string {
	t.Helper()

	tmpDir := t.TempDir()

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

// UniqueWorkspaceID generates a unique env key for a test.
func UniqueWorkspaceID(t *testing.T) string {
	t.Helper()
	// Use test name to generate a semi-unique key
	name := t.Name()
	if len(name) > 12 {
		name = name[:12]
	}
	return TestPrefix + name
}

// CreateTempComposeWorkspaceWithDcx creates a temp workspace with compose files and dcx.json.
func CreateTempComposeWorkspaceWithDcx(t *testing.T, devcontainerJSON, dockerComposeYAML, dcxJSON string) string {
	t.Helper()

	tmpDir := t.TempDir()

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

	// Write dcx.json
	dcxPath := filepath.Join(devcontainerDir, "dcx.json")
	err = os.WriteFile(dcxPath, []byte(dcxJSON), 0644)
	require.NoError(t, err)

	return tmpDir
}

// CreateTempWorkspaceWithDcx creates a temp workspace with devcontainer.json and dcx.json.
func CreateTempWorkspaceWithDcx(t *testing.T, devcontainerJSON, dcxJSON string) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Write devcontainer.json
	configPath := filepath.Join(devcontainerDir, "devcontainer.json")
	err = os.WriteFile(configPath, []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	// Write dcx.json
	dcxPath := filepath.Join(devcontainerDir, "dcx.json")
	err = os.WriteFile(dcxPath, []byte(dcxJSON), 0644)
	require.NoError(t, err)

	return tmpDir
}

// GetStatusField extracts a field value from dcx status output.
func GetStatusField(t *testing.T, statusOutput, fieldName string) string {
	t.Helper()

	for _, line := range strings.Split(statusOutput, "\n") {
		if strings.HasPrefix(line, fieldName+":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) >= 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}
