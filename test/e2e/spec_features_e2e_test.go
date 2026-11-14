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
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"overrideCommand": true
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Bring up - should not exit immediately even though alpine has no default command
	t.Run("container_stays_running_with_override", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Devcontainer started successfully")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "running", state)
	})

	// Container should respond to exec commands
	t.Run("exec_works_with_override", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "override-test")
		require.NoError(t, err)
		assert.Contains(t, stdout, "override-test")
	})

	// Verify the container is running a sleep process (our override command)
	t.Run("sleep_process_running", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "ps", "aux")
		require.NoError(t, err)
		// Should have a sleep process running
		assert.Contains(t, stdout, "sleep")
	})
}

// TestShutdownActionNoneE2E tests shutdownAction: "none".
func TestShutdownActionNoneE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Shutdown None Test",
		"image": "alpine:3.24",
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
	assert.Equal(t, "running", helpers.GetContainerState(t, workspace))

	// Stop without force should be skipped
	t.Run("stop_skipped_without_force", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Contains(t, stdout, "Skipping stop")

		// Container should still be running
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "running", state)
	})

	// Stop with force should work
	t.Run("stop_works_with_force", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop", "--force")
		assert.Contains(t, stdout, "stopped")

		// Container should be stopped
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "created", state)
	})
}

// TestShutdownActionStopContainerE2E tests shutdownAction: "stopContainer" (default).
func TestShutdownActionStopContainerE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Shutdown Stop Test",
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"overrideCommand": true,
		"shutdownAction": "stopContainer"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Equal(t, "running", helpers.GetContainerState(t, workspace))

	// Stop should work without force
	t.Run("stop_works_without_force", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Contains(t, stdout, "stopped")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "created", state)
	})
}

// TestHostRequirementsValidationE2E tests hostRequirements validation.
func TestHostRequirementsValidationE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// CPU and memory minimums the host can't meet warn and start anyway:
	// many repositories set Codespaces-sized ones.
	t.Run("warns_for_unmet_cpu_and_memory", func(t *testing.T) {
		devcontainerJSON := helpers.SimpleImageConfigWithHostReqs(t, "alpine:3.24", `"cpus": 10000, "memory": "10000gb"`)
		workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

		stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
		require.NoError(t, err, "unmet cpu/memory must not block up: stdout=%s stderr=%s", stdout, stderr)
		assert.Contains(t, stderr, "10000 CPUs recommended")
		assert.Contains(t, stderr, "10000gb of memory recommended")
	})

	// Test with achievable requirements (should succeed)
	t.Run("succeeds_with_achievable_requirements", func(t *testing.T) {
		devcontainerJSON := helpers.SimpleImageConfigWithHostReqs(t, "alpine:3.24", `"cpus": 1, "memory": "512mb"`)
		workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Devcontainer started successfully")
	})

	// A storage requirement can't be checked and is not reported.
	t.Run("ignores_storage_requirement", func(t *testing.T) {
		devcontainerJSON := helpers.SimpleImageConfigWithHostReqs(t, "alpine:3.24", `"storage": "10gb"`)
		workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

		stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
		require.NoError(t, err, "should succeed with storage requirement")
		assert.Contains(t, stdout, "Devcontainer started successfully")
		assert.NotContains(t, stderr, "torage")
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

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test that exec runs as remoteUser
	t.Run("exec_runs_as_remote_user", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "whoami")
		require.NoError(t, err)
		assert.Contains(t, stdout, "node")
	})
}

// TestForwardPortsE2E tests forwardPorts configuration.
func TestForwardPortsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Ports Test",
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"overrideCommand": true,
		"forwardPorts": [8080, "9000:9000"],
		"appPort": [7071]
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Get container name from status
	containerName := helpers.ContainerName(t, workspace)

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
		assert.Contains(t, portInfo, "7071", "appPort creates (Docker rejected the literal \"localhost\")")
	})

	// Ports bind loopback unless the config names an address: dev servers
	// must not be reachable from the network by default.
	t.Run("ports_bind_loopback", func(t *testing.T) {
		out, err := exec.Command("docker", "port", containerName).CombinedOutput()
		require.NoError(t, err, string(out))
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			assert.Contains(t, line, "127.0.0.1:", "published beyond loopback: %s", line)
		}
	})
}
