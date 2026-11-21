//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserEnvProbeE2E: lifecycle hooks get the environment the
// userEnvProbe captured.
func TestUserEnvProbeE2E(t *testing.T) {
	t.Parallel()
	stdout, _, err := helpers.RunDCXInDir(t, debianEnv(t).Dir, "ssh", "--", "cat", "/tmp/probe-result.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "PATH=", "probed env should contain PATH")
	assert.Contains(t, stdout, "HOME=", "probed env should contain HOME")
}

// TestEtcProfilePatchingE2E: /etc/profile is patched once to keep PATH.
func TestEtcProfilePatchingE2E(t *testing.T) {
	t.Parallel()
	stdout, _, err := helpers.RunDCXInDir(t, debianEnv(t).Dir, "ssh", "--", "sh", "-c", "test -f /var/lib/dcx/.patchEtcProfileMarker && echo patched || echo not-patched")
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
