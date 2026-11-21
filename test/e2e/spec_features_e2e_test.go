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

// TestHostRequirementsValidationE2E: CPU and memory minimums the host
// can't meet warn and start anyway (many repositories set Codespaces-sized
// ones); a required GPU on a host without a GPU runtime warns and starts
// without GPUs, as the official CLI does (R6/C12); a storage requirement
// can't be checked and is not reported.
func TestHostRequirementsValidationE2E(t *testing.T) {
	t.Parallel()
	up := richEnv(t).UpOutput
	assert.Contains(t, up, "10000 CPUs recommended")
	assert.Contains(t, up, "10000gb of memory recommended")
	if !richGPURuntime {
		assert.Contains(t, up, "no GPU runtime")
	}
	assert.NotContains(t, up, "torage")
	assert.Contains(t, up, "Devcontainer started successfully")
}

// TestRemoteUserE2E: sessions run as remoteUser.
func TestRemoteUserE2E(t *testing.T) {
	t.Parallel()
	stdout, _, err := helpers.RunDCXInDir(t, nodeEnv(t).Dir, "ssh", "--", "whoami")
	require.NoError(t, err)
	assert.Contains(t, stdout, "node")
}

// TestForwardPortsE2E: forwardPorts and appPort are published, on loopback.
func TestForwardPortsE2E(t *testing.T) {
	t.Parallel()
	containerName := helpers.ContainerName(t, richEnv(t).Dir)

	output, err := exec.Command("docker", "inspect", "--format",
		`{{range $p, $conf := .NetworkSettings.Ports}}{{$p}} {{end}}`, containerName).CombinedOutput()
	require.NoError(t, err)
	assert.Contains(t, string(output), "8080")
	assert.Contains(t, string(output), "9000")
	assert.Contains(t, string(output), "7071", "appPort creates (Docker rejected the literal \"localhost\")")

	out, err := exec.Command("docker", "port", containerName).CombinedOutput()
	require.NoError(t, err, string(out))
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		assert.Contains(t, line, "127.0.0.1:", "published beyond loopback: %s", line)
	}
}
