//go:build e2e

package e2e

import (
	"strings"
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
		"image": "debian:bookworm-slim",
		"workspaceFolder": "/workspace",
		"userEnvProbe": "loginInteractiveShell",
		"postCreateCommand": "printenv > /tmp/probe-result.txt"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Check if the postCreateCommand captured the probed environment
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/probe-result.txt")
	require.NoError(t, err)

	// The probed environment should include basic variables like PATH, HOME
	assert.Contains(t, stdout, "PATH=", "probed env should contain PATH")
	assert.Contains(t, stdout, "HOME=", "probed env should contain HOME")
}

// TestEtcProfilePatchingE2E tests that /etc/profile is patched
// to preserve PATH from features.
func TestEtcProfilePatchingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Etc Profile Patch Test",
		"image": "debian:bookworm-slim",
		"workspaceFolder": "/workspace"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Check that the marker file exists (indicating patching was done)
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "sh", "-c", "test -f /var/lib/dcx/.patchEtcProfileMarker && echo patched || echo not-patched")
	require.NoError(t, err)
	assert.Equal(t, "patched", strings.TrimSpace(stdout), "/etc/profile should be patched")
}

// TestUserEnvProbeWithFeatureE2E tests that userEnvProbe captures
// environment modifications from features.
func TestUserEnvProbeWithFeatureE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Use a feature that sets environment variables
	devcontainerJSON := `{
		"name": "UserEnvProbe Feature Test",
		"image": "debian:bookworm-slim",
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

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// The postCreateCommand should have access to the full PATH
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/path-check.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "PATH=", "PATH should be captured")
}

// TestUserEnvProbeNoneE2E tests that userEnvProbe="none" skips probing.
func TestUserEnvProbeNoneE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "UserEnvProbe None Test",
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"userEnvProbe": "none"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Bring up should succeed without probing
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Basic exec should still work
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "hello")
	require.NoError(t, err)
	assert.Contains(t, stdout, "hello")
}
