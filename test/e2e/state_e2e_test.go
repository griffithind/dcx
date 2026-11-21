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

// TestStateLifecycleE2E walks one workspace through every state (absent,
// running, stopped, and back), checking in each what state, status, doctor,
// ssh, stop and down do there.
func TestStateLifecycleE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := helpers.CreateTempWorkspace(t, helpers.SimpleImageConfig(t, "alpine:3.24"))
	state := func() string { return helpers.GetContainerState(t, workspace) }

	t.Run("absent", func(t *testing.T) {
		assert.Equal(t, "absent", state())
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		for _, want := range []string{"State:", "absent", "Workspace:", "Workspace ID:"} {
			assert.Contains(t, stdout, want)
		}
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "doctor")
		require.NoError(t, err, "doctor works without a container")
		assert.Contains(t, stdout, "Docker")
		_, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "test")
		assert.Error(t, err, "ssh fails when no container exists")
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "down"), "No devcontainer found", "down is idempotent")
	})

	t.Run("absent_to_running", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Equal(t, "running", state())
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		for _, want := range []string{"State:", "running", "Primary Container", "ID:", "Name:"} {
			assert.Contains(t, stdout, want)
		}
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "doctor")
		require.NoError(t, err)
		assert.Contains(t, stdout, "Docker")
	})

	t.Run("running_to_stopped", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Equal(t, "created", state())
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.Contains(t, stdout, "State:")
		assert.Contains(t, stdout, "created")
		_, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "test")
		assert.Error(t, err, "ssh fails on a stopped container")
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "stop"), "already stopped", "stop is idempotent")
	})

	t.Run("stopped_to_running", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Equal(t, "running", state())
	})

	t.Run("running_to_absent", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Equal(t, "absent", state())
	})

	t.Run("up_from_absent", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Equal(t, "running", state())
	})

	t.Run("stopped_to_absent", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Equal(t, "created", state())
		helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Equal(t, "absent", state())
	})
}

// TestStaleDetectionE2E tests that config changes result in stale state.
func TestStaleDetectionE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create initial config
	devcontainerJSON := `{
		"name": "Stale Test",
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	state := helpers.GetContainerState(t, workspace)
	assert.Equal(t, "running", state)

	// Modify devcontainer.json
	modifiedConfig := `{
		"name": "Stale Test Modified",
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"containerEnv": {
			"NEW_VAR": "value"
		}
	}`

	configPath := filepath.Join(workspace, ".devcontainer", "devcontainer.json")
	err := os.WriteFile(configPath, []byte(modifiedConfig), 0644)
	require.NoError(t, err)

	// Check state - should be stale
	t.Run("detects_stale_config", func(t *testing.T) {
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "stale", state)
	})

	// Running 'up' should recreate
	t.Run("up_recreates_stale", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "running", state)
	})

	// Verify new env var exists
	t.Run("new_config_applied", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "printenv", "NEW_VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "value")
	})
}
