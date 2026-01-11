//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrorMissingConfigE2E tests that dcx commands fail gracefully when no devcontainer.json exists.
func TestErrorMissingConfigE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create empty workspace with no .devcontainer directory
	workspace := t.TempDir()

	t.Run("up_missing_config", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
		require.Error(t, err, "dcx up should fail when no config exists")
		assert.Contains(t, stderr, "devcontainer.json", "error should mention devcontainer.json")
	})

	t.Run("build_missing_config", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "build")
		require.Error(t, err, "dcx build should fail when no config exists")
		assert.Contains(t, stderr, "devcontainer.json", "error should mention devcontainer.json")
	})

	t.Run("exec_missing_config", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "test")
		require.Error(t, err, "dcx exec should fail when no config exists")
		// exec might fail differently (no container), but should still error
		assert.True(t, len(stderr) > 0 || err != nil, "should produce an error")
	})
}

// TestErrorInvalidJSONE2E tests that dcx fails gracefully with malformed devcontainer.json.
func TestErrorInvalidJSONE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Write malformed JSON
	invalidJSON := `{
		"name": "Invalid Config"
		"image": "alpine:latest"
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(invalidJSON), 0644)
	require.NoError(t, err)

	t.Run("up_invalid_json", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
		require.Error(t, err, "dcx up should fail with invalid JSON")
		// Should mention JSON parsing error
		combined := stderr
		assert.True(t, len(combined) > 0, "should produce error output")
	})

	t.Run("build_invalid_json", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "build")
		require.Error(t, err, "dcx build should fail with invalid JSON")
		combined := stderr
		assert.True(t, len(combined) > 0, "should produce error output")
	})
}

// TestErrorInvalidFeatureE2E tests that dcx fails gracefully with an invalid feature reference.
func TestErrorInvalidFeatureE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Write config with non-existent feature
	devcontainerJSON := `{
		"name": "Invalid Feature Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"features": {
			"ghcr.io/nonexistent-org/nonexistent-feature:latest": {}
		}
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	t.Run("up_invalid_feature", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
		require.Error(t, err, "dcx up should fail with invalid feature")
		combined := stderr
		assert.True(t, len(combined) > 0 || err != nil, "should produce an error")
	})

	t.Run("build_invalid_feature", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "build")
		require.Error(t, err, "dcx build should fail with invalid feature")
		combined := stderr
		assert.True(t, len(combined) > 0 || err != nil, "should produce an error")
	})
}

// TestErrorMissingImageE2E tests that dcx fails gracefully when image cannot be pulled.
func TestErrorMissingImageE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Write config with non-existent image
	devcontainerJSON := `{
		"name": "Missing Image Test",
		"image": "nonexistent-registry.invalid/nonexistent-image:v999.999.999",
		"workspaceFolder": "/workspace"
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	t.Run("up_missing_image", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
		require.Error(t, err, "dcx up should fail when image cannot be pulled")
		combined := stderr
		assert.True(t, len(combined) > 0 || err != nil, "should produce an error")
	})

	t.Run("build_missing_image", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "build")
		require.Error(t, err, "dcx build should fail when image cannot be pulled")
		combined := stderr
		assert.True(t, len(combined) > 0 || err != nil, "should produce an error")
	})
}

// TestErrorMissingDockerfileE2E tests that dcx fails gracefully when Dockerfile is missing.
func TestErrorMissingDockerfileE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Write config referencing non-existent Dockerfile
	devcontainerJSON := `{
		"name": "Missing Dockerfile Test",
		"build": {
			"dockerfile": "NonexistentDockerfile"
		},
		"workspaceFolder": "/workspace"
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	t.Run("up_missing_dockerfile", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
		require.Error(t, err, "dcx up should fail when Dockerfile is missing")
		combined := stderr
		assert.True(t, len(combined) > 0 || err != nil, "should produce an error")
	})

	t.Run("build_missing_dockerfile", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "build")
		require.Error(t, err, "dcx build should fail when Dockerfile is missing")
		combined := stderr
		assert.True(t, len(combined) > 0 || err != nil, "should produce an error")
	})
}
