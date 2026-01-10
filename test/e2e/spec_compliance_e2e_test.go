//go:build e2e

package e2e

import (
	"fmt"
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
		"image": "alpine:latest",
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
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "REMOTE_VAR_1")
		require.NoError(t, err)
		assert.Contains(t, stdout, "remote-value-1")
	})

	t.Run("multiple_remoteEnv_vars", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "printenv REMOTE_VAR_1 && printenv REMOTE_VAR_2")
		require.NoError(t, err)
		assert.Contains(t, stdout, "remote-value-1")
		assert.Contains(t, stdout, "remote-value-2")
	})

	t.Run("remoteEnv_EDITOR", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "EDITOR")
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
		"image": "alpine:latest",
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

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	t.Run("containerEnv_is_available", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "CONTAINER_VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "container-value")
	})

	t.Run("remoteEnv_is_available", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "REMOTE_VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "remote-value")
	})

	t.Run("remoteEnv_can_override_containerEnv", func(t *testing.T) {
		// Per spec, remoteEnv is applied per session and should override containerEnv
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "SHARED_VAR")
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
		"image": "alpine:latest",
		"workspaceFolder": "/workspace"
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container - should succeed because overrideCommand defaults to true
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify the container is running by executing a command
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "container-is-running")
	require.NoError(t, err)
	assert.Contains(t, stdout, "container-is-running")
}

// TestOverrideCommandExplicitFalseE2E tests that setting overrideCommand to false
// does not override the container's command.
func TestOverrideCommandExplicitFalseE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Use an image with a command that keeps running (alpine with a sleep)
	// and explicitly set overrideCommand to false
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"overrideCommand": false,
		"runArgs": ["--entrypoint", "sleep", "-c", "infinity"]
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// The container should still run because we're using runArgs to set entrypoint
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify the container is running
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "still-running")
	require.NoError(t, err)
	assert.Contains(t, stdout, "still-running")
}

// TestWaitForDefaultE2E tests the default waitFor behavior (updateContentCommand per spec).
// Note: This is a basic test to ensure the container comes up correctly with default waitFor.
func TestWaitForDefaultE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"onCreateCommand": "echo 'onCreate executed'",
		"updateContentCommand": "echo 'updateContent executed'",
		"postCreateCommand": "echo 'postCreate executed'"
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

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
