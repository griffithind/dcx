//go:build e2e

package e2e

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOverrideCommandE2E tests the overrideCommand feature.
func TestOverrideCommandE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create config with overrideCommand: true
	devcontainerJSON := `{
		"name": "Override Command Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"overrideCommand": true
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up - should not exit immediately even though alpine has no default command
	t.Run("container_stays_running_with_override", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// Container should respond to exec commands
	t.Run("exec_works_with_override", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "override-test")
		require.NoError(t, err)
		assert.Contains(t, stdout, "override-test")
	})

	// Verify the container is running a sleep process (our override command)
	t.Run("sleep_process_running", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "ps", "aux")
		require.NoError(t, err)
		// Should have a sleep process running
		assert.Contains(t, stdout, "sleep")
	})
}

// TestOverrideCommandFalseE2E tests that overrideCommand: false uses default entrypoint.
func TestOverrideCommandFalseE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// alpine with explicit overrideCommand: false should use image's CMD (/bin/sh)
	// With TTY enabled (standard for devcontainers), the shell stays alive
	devcontainerJSON := `{
		"name": "No Override Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"overrideCommand": false
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up - alpine runs /bin/sh which stays alive with TTY
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Container should still be running because TTY keeps shell alive
	t.Run("container_runs_with_default_cmd", func(t *testing.T) {
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// Verify we're running the default command (sh), not the override (sleep loop)
	t.Run("runs_default_shell_not_sleep_loop", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "ps", "aux")
		require.NoError(t, err)
		// Should have /bin/sh as PID 1, not /bin/sh -c "while sleep..."
		assert.Contains(t, stdout, "/bin/sh")
		// Should NOT have the sleep loop pattern
		assert.NotContains(t, stdout, "while sleep")
	})
}

// TestShutdownActionNoneE2E tests shutdownAction: "none".
func TestShutdownActionNoneE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Shutdown None Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"overrideCommand": true,
		"shutdownAction": "none"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		// Force down to clean up
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Equal(t, "RUNNING", helpers.GetContainerState(t, workspace))

	// Stop without force should be skipped
	t.Run("stop_skipped_without_force", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Contains(t, stdout, "Skipping stop")

		// Container should still be running
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// Stop with force should work
	t.Run("stop_works_with_force", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop", "--force")
		assert.Contains(t, stdout, "stopped")

		// Container should be stopped
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "CREATED", state)
	})
}

// TestShutdownActionStopContainerE2E tests shutdownAction: "stopContainer" (default).
func TestShutdownActionStopContainerE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Shutdown Stop Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"overrideCommand": true,
		"shutdownAction": "stopContainer"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Equal(t, "RUNNING", helpers.GetContainerState(t, workspace))

	// Stop should work without force
	t.Run("stop_works_without_force", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Contains(t, stdout, "stopped")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "CREATED", state)
	})
}

// TestHostRequirementsValidationE2E tests hostRequirements validation.
func TestHostRequirementsValidationE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Test with impossible CPU requirement (should fail)
	t.Run("fails_with_impossible_cpu", func(t *testing.T) {
		devcontainerJSON := `{
			"name": "CPU Test",
			"image": "alpine:latest",
			"workspaceFolder": "/workspace",
			"hostRequirements": {
				"cpus": 10000
			}
		}`
		workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

		t.Cleanup(func() {
			helpers.RunDCXInDir(t, workspace, "down")
		})

		stdout, _, err := helpers.RunDCXInDir(t, workspace, "up")
		assert.Error(t, err, "should fail with impossible CPU requirement")
		// Error messages go to stdout in dcx
		assert.Contains(t, stdout, "CPU requirement not met")
	})

	// Test with impossible memory requirement (should fail)
	t.Run("fails_with_impossible_memory", func(t *testing.T) {
		devcontainerJSON := `{
			"name": "Memory Test",
			"image": "alpine:latest",
			"workspaceFolder": "/workspace",
			"hostRequirements": {
				"memory": "10000gb"
			}
		}`
		workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

		t.Cleanup(func() {
			helpers.RunDCXInDir(t, workspace, "down")
		})

		stdout, _, err := helpers.RunDCXInDir(t, workspace, "up")
		assert.Error(t, err, "should fail with impossible memory requirement")
		// Error messages go to stdout in dcx
		assert.Contains(t, stdout, "Memory requirement not met")
	})

	// Test with achievable requirements (should succeed)
	t.Run("succeeds_with_achievable_requirements", func(t *testing.T) {
		devcontainerJSON := `{
			"name": "Achievable Test",
			"image": "alpine:latest",
			"workspaceFolder": "/workspace",
			"overrideCommand": true,
			"hostRequirements": {
				"cpus": 1,
				"memory": "512mb"
			}
		}`
		workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

		t.Cleanup(func() {
			helpers.RunDCXInDir(t, workspace, "down")
		})

		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")
	})

	// Test with storage requirement (should warn but succeed)
	t.Run("warns_for_storage_requirement", func(t *testing.T) {
		devcontainerJSON := `{
			"name": "Storage Test",
			"image": "alpine:latest",
			"workspaceFolder": "/workspace",
			"overrideCommand": true,
			"hostRequirements": {
				"storage": "10gb"
			}
		}`
		workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

		t.Cleanup(func() {
			helpers.RunDCXInDir(t, workspace, "down")
		})

		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "cannot be validated")
	})
}

// TestRemoteUserE2E tests remoteUser configuration.
func TestRemoteUserE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Use an image with a non-root user
	devcontainerJSON := `{
		"name": "Remote User Test",
		"image": "node:20-alpine",
		"workspaceFolder": "/workspace",
		"remoteUser": "node",
		"overrideCommand": true
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test that exec runs as remoteUser
	t.Run("exec_runs_as_remote_user", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "whoami")
		require.NoError(t, err)
		assert.Contains(t, stdout, "node")
	})
}

// TestContainerEnvE2E tests containerEnv configuration.
func TestContainerEnvE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Spec Container Env Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"overrideCommand": true,
		"containerEnv": {
			"MY_CUSTOM_VAR": "custom-value-123",
			"ANOTHER_VAR": "another-value"
		}
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	t.Run("container_env_vars_set", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "MY_CUSTOM_VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "custom-value-123")

		stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "ANOTHER_VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "another-value")
	})
}

// TestForwardPortsE2E tests forwardPorts configuration.
func TestForwardPortsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Ports Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"overrideCommand": true,
		"forwardPorts": [8080, "9000:9000"]
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Get container name from status
	statusOut := helpers.RunDCXInDirSuccess(t, workspace, "status")
	var containerName string
	for _, line := range strings.Split(statusOut, "\n") {
		if strings.Contains(line, "Name:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				containerName = parts[1]
			}
		}
	}
	require.NotEmpty(t, containerName)

	// Verify ports are exposed
	t.Run("ports_are_exposed", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{range $p, $conf := .NetworkSettings.Ports}}{{$p}} {{end}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err)

		portInfo := string(output)
		assert.Contains(t, portInfo, "8080")
		assert.Contains(t, portInfo, "9000")
	})
}
