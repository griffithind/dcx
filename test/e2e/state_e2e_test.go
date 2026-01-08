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

// TestStateTransitionsE2E tests all valid state transitions.
func TestStateTransitionsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// ABSENT -> RUNNING (via up)
	t.Run("absent_to_running", func(t *testing.T) {
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "ABSENT", state)

		helpers.RunDCXInDirSuccess(t, workspace, "up")

		state = helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// RUNNING -> CREATED (via stop)
	t.Run("running_to_created", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "stop")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "CREATED", state)
	})

	// CREATED -> RUNNING (via up - smart start)
	t.Run("created_to_running", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// RUNNING -> ABSENT (via down)
	t.Run("running_to_absent", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "down")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "ABSENT", state)
	})

	// ABSENT -> RUNNING again
	t.Run("absent_to_running_again", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// CREATED -> ABSENT (via down from stopped state)
	t.Run("created_to_absent", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "stop")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "CREATED", state)

		helpers.RunDCXInDirSuccess(t, workspace, "down")

		state = helpers.GetContainerState(t, workspace)
		assert.Equal(t, "ABSENT", state)
	})
}

// TestStaleDetectionE2E tests that config changes result in STALE state.
func TestStaleDetectionE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create initial config
	devcontainerJSON := `{
		"name": "Stale Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	state := helpers.GetContainerState(t, workspace)
	assert.Equal(t, "RUNNING", state)

	// Modify devcontainer.json
	modifiedConfig := `{
		"name": "Stale Test Modified",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"containerEnv": {
			"NEW_VAR": "value"
		}
	}`

	configPath := filepath.Join(workspace, ".devcontainer", "devcontainer.json")
	err := os.WriteFile(configPath, []byte(modifiedConfig), 0644)
	require.NoError(t, err)

	// Check state - should be STALE
	t.Run("detects_stale_config", func(t *testing.T) {
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "STALE", state)
	})

	// Running 'up' should recreate
	t.Run("up_recreates_stale", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// Verify new env var exists
	t.Run("new_config_applied", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "NEW_VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "value")
	})
}

// TestUpFromAbsentE2E tests that up works when no container exists.
func TestUpFromAbsentE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Ensure nothing exists
	helpers.RunDCXInDir(t, workspace, "down")

	// Up should succeed and create container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	state := helpers.GetContainerState(t, workspace)
	assert.Equal(t, "RUNNING", state)
}

// TestExecOnStoppedFailsE2E tests that exec fails on stopped container.
func TestExecOnStoppedFailsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Create and stop
	helpers.RunDCXInDirSuccess(t, workspace, "up")
	helpers.RunDCXInDirSuccess(t, workspace, "stop")

	// Exec should fail
	_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "test")
	assert.Error(t, err, "exec should fail on stopped container")
}

// TestExecOnAbsentFailsE2E tests that exec fails when no container exists.
func TestExecOnAbsentFailsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Ensure nothing exists
	helpers.RunDCXInDir(t, workspace, "down")

	// Exec should fail
	_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "test")
	assert.Error(t, err, "exec should fail when no container exists")
}

// TestStopIdempotentE2E tests that stop is idempotent.
func TestStopIdempotentE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Create and stop
	helpers.RunDCXInDirSuccess(t, workspace, "up")
	helpers.RunDCXInDirSuccess(t, workspace, "stop")

	// Second stop should succeed
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop")
	assert.Contains(t, stdout, "already stopped")
}

// TestDownIdempotentE2E tests that down is idempotent.
func TestDownIdempotentE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Ensure nothing exists
	helpers.RunDCXInDir(t, workspace, "down")

	// Down on absent should succeed
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "down")
	assert.Contains(t, stdout, "No environment found")
}

// TestStatusCommandE2E tests the status command output.
func TestStatusCommandE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Status when absent
	t.Run("status_absent", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.Contains(t, stdout, "State:")
		assert.Contains(t, stdout, "ABSENT")
		assert.Contains(t, stdout, "Workspace:")
		assert.Contains(t, stdout, "Env Key:")
	})

	// Status when running
	t.Run("status_running", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")

		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.Contains(t, stdout, "State:")
		assert.Contains(t, stdout, "RUNNING")
		assert.Contains(t, stdout, "Primary Container:")
		assert.Contains(t, stdout, "ID:")
		assert.Contains(t, stdout, "Name:")
	})

	// Status when stopped
	t.Run("status_created", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "stop")

		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.Contains(t, stdout, "State:")
		assert.Contains(t, stdout, "CREATED")
	})
}

// TestDoctorCommandE2E tests the doctor command.
func TestDoctorCommandE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Doctor should work even when no container exists
	t.Run("doctor_absent", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "doctor")
		require.NoError(t, err)
		assert.Contains(t, stdout, "Docker")
	})

	// Doctor with running container
	t.Run("doctor_running", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")

		stdout, _, err := helpers.RunDCXInDir(t, workspace, "doctor")
		require.NoError(t, err)
		assert.Contains(t, stdout, "Docker")
	})
}
