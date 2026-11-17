//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoteEnvE2E tests that remoteEnv variables are applied to exec sessions.
// Per devcontainer spec, remoteEnv is set for user sessions (docker exec) not at container creation.
func TestRemoteEnvE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"remoteEnv": {
			"REMOTE_VAR_1": "remote-value-1",
			"REMOTE_VAR_2": "remote-value-2",
			"EDITOR": "vim"
		}
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	t.Run("remoteEnv_is_available_in_exec", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "printenv", "REMOTE_VAR_1")
		require.NoError(t, err)
		assert.Contains(t, stdout, "remote-value-1")
	})

	t.Run("multiple_remoteEnv_vars", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "sh", "-c", "printenv REMOTE_VAR_1 && printenv REMOTE_VAR_2")
		require.NoError(t, err)
		assert.Contains(t, stdout, "remote-value-1")
		assert.Contains(t, stdout, "remote-value-2")
	})

	t.Run("remoteEnv_EDITOR", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "printenv", "EDITOR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "vim")
	})
}

// TestRemoteEnvAndContainerEnvE2E tests that both containerEnv and remoteEnv work together.
// containerEnv is set at container creation, remoteEnv is set per exec session.
func TestRemoteEnvAndContainerEnvE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"containerEnv": {
			"CONTAINER_VAR": "container-value",
			"SHARED_VAR": "from-container"
		},
		"remoteEnv": {
			"REMOTE_VAR": "remote-value",
			"SHARED_VAR": "from-remote"
		}
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	t.Run("containerEnv_is_available", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "printenv", "CONTAINER_VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "container-value")
	})

	t.Run("remoteEnv_is_available", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "printenv", "REMOTE_VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "remote-value")
	})

	t.Run("remoteEnv_can_override_containerEnv", func(t *testing.T) {
		// Per spec, remoteEnv is applied per session and should override containerEnv
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "printenv", "SHARED_VAR")
		require.NoError(t, err)
		// remoteEnv should take precedence in exec sessions
		assert.Contains(t, stdout, "from-remote")
	})
}

// TestOverrideCommandDefaultImageE2E tests that image-based containers stay running
// by default (overrideCommand defaults to true for image-based configs).
func TestOverrideCommandDefaultImageE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Use an image that would normally exit (no long-running process)
	// Without overrideCommand=true (or default true), this would exit immediately
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace"
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Bring up the container - should succeed because overrideCommand defaults to true
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify the container is running by executing a command
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "container-is-running")
	require.NoError(t, err)
	assert.Contains(t, stdout, "container-is-running")
}

// TestOverrideCommandExplicitFalseE2E tests that setting overrideCommand to false
// does not override the container's command.
func TestOverrideCommandExplicitFalseE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// With overrideCommand=false dcx must not replace the image's command
	// with its keep-alive: PID 1 is the image's own command.
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"overrideCommand": false
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".devcontainer", "Dockerfile"),
		[]byte("FROM alpine:3.24\nCMD [\"tail\", \"-f\", \"/dev/null\"]\n"), 0o644))

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "sh", "-c", "tr '\\0' ' ' < /proc/1/cmdline")
	require.NoError(t, err)
	assert.Contains(t, stdout, "tail -f /dev/null")
	assert.NotContains(t, stdout, "dcx-keepalive")
}

// TestOverrideCommandFalseWithoutTTYE2E: the container gets no TTY or
// open stdin (R13), as devcontainers/cli creates it, so an image command
// that needs a terminal (alpine's /bin/sh) exits and up says so.
func TestOverrideCommandFalseWithoutTTYE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"overrideCommand": false
	}`, helpers.UniqueTestName(t)))

	_, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
	require.Error(t, err)
	assert.Contains(t, stderr, "not running")
}

// TestWaitForDefaultE2E tests the default waitFor behavior (updateContentCommand per spec).
// Note: This is a basic test to ensure the container comes up correctly with default waitFor.
func TestWaitForDefaultE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"onCreateCommand": "echo 'onCreate executed'",
		"updateContentCommand": "echo 'updateContent executed'",
		"postCreateCommand": "echo 'postCreate executed'"
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Bring up the container
	// With default waitFor=updateContentCommand, the up should wait for:
	// - initializeCommand
	// - onCreateCommand
	// - updateContentCommand
	// But NOT for postCreateCommand (runs in background)
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify lifecycle commands ran
	assert.Contains(t, stdout, "onCreate")
	assert.Contains(t, stdout, "updateContent")
}
