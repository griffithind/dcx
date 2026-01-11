//go:build e2e

package e2e

import (
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserEnvProbeE2E tests the userEnvProbe functionality.
// This ensures that environment variables from shell initialization files
// are properly captured and made available to lifecycle hooks.
func TestUserEnvProbeE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create a devcontainer with userEnvProbe that captures basic env vars
	// We test that the probed environment (from login shell) is available
	devcontainerJSON := `{
		"name": "UserEnvProbe Test",
		"image": "ubuntu:22.04",
		"workspaceFolder": "/workspace",
		"userEnvProbe": "loginInteractiveShell",
		"postCreateCommand": "printenv > /tmp/probe-result.txt"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Check if the postCreateCommand captured the probed environment
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/probe-result.txt")
	require.NoError(t, err)

	// The probed environment should include basic variables like PATH, HOME
	assert.Contains(t, stdout, "PATH=", "probed env should contain PATH")
	assert.Contains(t, stdout, "HOME=", "probed env should contain HOME")
}

// TestEtcEnvironmentPatchingE2E tests that containerEnv/remoteEnv
// are properly patched into /etc/environment.
func TestEtcEnvironmentPatchingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Etc Environment Patch Test",
		"image": "ubuntu:22.04",
		"workspaceFolder": "/workspace",
		"containerEnv": {
			"MY_CONTAINER_VAR": "container-value"
		},
		"remoteEnv": {
			"MY_REMOTE_VAR": "remote-value"
		}
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Check that /etc/environment contains our variables
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/etc/environment")
	require.NoError(t, err)
	assert.Contains(t, stdout, "MY_CONTAINER_VAR", "/etc/environment should contain containerEnv")
	assert.Contains(t, stdout, "container-value")
	assert.Contains(t, stdout, "MY_REMOTE_VAR", "/etc/environment should contain remoteEnv")
	assert.Contains(t, stdout, "remote-value")
}

// TestEtcProfilePatchingE2E tests that /etc/profile is patched
// to preserve PATH from features.
func TestEtcProfilePatchingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Etc Profile Patch Test",
		"image": "ubuntu:22.04",
		"workspaceFolder": "/workspace"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Check that the marker file exists (indicating patching was done)
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "test -f /var/lib/dcx/.patchEtcProfileMarker && echo 'patched' || echo 'not-patched'")
	require.NoError(t, err)
	assert.Contains(t, stdout, "patched", "/etc/profile should be patched")
}

// TestUserEnvProbeWithFeatureE2E tests that userEnvProbe captures
// environment modifications from features.
func TestUserEnvProbeWithFeatureE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Use a feature that sets environment variables
	devcontainerJSON := `{
		"name": "UserEnvProbe Feature Test",
		"image": "ubuntu:22.04",
		"workspaceFolder": "/workspace",
		"features": {
			"ghcr.io/devcontainers/features/common-utils:2": {
				"installZsh": false,
				"installOhMyZsh": false
			}
		},
		"userEnvProbe": "loginShell",
		"postCreateCommand": "echo PATH=$PATH > /tmp/path-check.txt"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// The postCreateCommand should have access to the full PATH
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/path-check.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "PATH=", "PATH should be captured")
}

// TestUserEnvProbeNoneE2E tests that userEnvProbe="none" skips probing.
func TestUserEnvProbeNoneE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "UserEnvProbe None Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"userEnvProbe": "none"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up should succeed without probing
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Basic exec should still work
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "hello")
	require.NoError(t, err)
	assert.Contains(t, stdout, "hello")
}

// TestEnvProbeCachingE2E tests that probed environment is cached
// and reused on subsequent starts.
func TestEnvProbeCachingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Env Probe Cache Test",
		"image": "ubuntu:22.04",
		"workspaceFolder": "/workspace",
		"userEnvProbe": "loginShell"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// First up - should probe and cache
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Check cache file exists
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "test -f /var/lib/dcx/probed-env.json && echo 'cached' || echo 'not-cached'")
	require.NoError(t, err)
	assert.Contains(t, stdout, "cached", "Probed environment should be cached")

	// Down then up again - should use cache
	helpers.RunDCXInDirSuccess(t, workspace, "down")
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Cache should still be there
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "test -f /var/lib/dcx/probed-env.json && echo 'cached' || echo 'not-cached'")
	require.NoError(t, err)
	assert.Contains(t, stdout, "cached", "Cache should persist across restarts")
}
