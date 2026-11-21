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

// TestHostRequirementsValidationE2E: CPU and memory minimums the host
// can't meet warn and start anyway (many repositories set Codespaces-sized
// ones); a required GPU on a host without a GPU runtime warns and starts
// without GPUs, as the official CLI does (R6/C12); a storage requirement
// can't be checked and is not reported.
func TestHostRequirementsValidationE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	reqs := `"cpus": 10000, "memory": "10000gb", "storage": "10gb"`
	gpuRuntime := false
	if out, err := exec.Command("docker", "info", "--format", "{{json .Runtimes}}").Output(); err == nil && strings.Contains(string(out), "nvidia") {
		gpuRuntime = true
	} else {
		reqs += `, "gpu": true`
	}
	workspace := helpers.CreateTempWorkspace(t, helpers.SimpleImageConfigWithHostReqs(t, "alpine:3.24", reqs))

	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
	require.NoError(t, err, "unmet requirements must not block up: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stderr, "10000 CPUs recommended")
	assert.Contains(t, stderr, "10000gb of memory recommended")
	if !gpuRuntime {
		assert.Contains(t, stderr, "no GPU runtime")
	}
	assert.NotContains(t, stderr, "torage")
	assert.Contains(t, stderr, "Devcontainer started successfully", "progress goes to stderr")
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
