//go:build e2e

package e2e

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	dcxembed "github.com/griffithind/dcx"
	"github.com/griffithind/dcx/internal/agentctl"
	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInstallAgentE2E checks the agent dcx up deployed: an executable
// binary at agentproto.BinaryPath, built for the container's architecture;
// deploying it again is a no-op (only the recorded digest is compared).
func TestInstallAgentE2E(t *testing.T) {
	t.Parallel()

	binary, err := dcxembed.Binary("amd64")
	if err != nil || len(binary) < 1024*1024 {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	containerName := helpers.ContainerName(t, alpineEnv(t).Dir)
	require.NotEmpty(t, containerName, "should find container name")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	binaryPath := agentproto.BinaryPath

	for i := range 2 {
		updated, err := agentctl.Deploy(ctx, container.NewDockerCLI(), containerName, false, false)
		require.NoError(t, err, "deploy %d", i+1)
		assert.False(t, updated, "deploy %d: dcx up already installed the current agent", i+1)
	}

	require.NoError(t, exec.CommandContext(ctx, "docker", "exec", containerName, "test", "-f", binaryPath).Run(),
		"binary should exist in container at %s", binaryPath)
	require.NoError(t, exec.CommandContext(ctx, "docker", "exec", containerName, "test", "-x", binaryPath).Run(),
		"binary should be executable")

	archOutput, err := exec.CommandContext(ctx, "docker", "exec", containerName, "uname", "-m").Output()
	require.NoError(t, err)
	containerArch := strings.TrimSpace(string(archOutput))
	output, err := exec.CommandContext(ctx, "docker", "exec", containerName, binaryPath, "--help").CombinedOutput()
	require.NoError(t, err, "binary should run on %s: %s", containerArch, string(output))
	assert.Contains(t, string(output), "dcx-agent", "help output should contain 'dcx-agent'")

	// The ELF header names the architecture (when `file` can be installed).
	fileOutput, err := exec.CommandContext(ctx, "docker", "exec", containerName, "sh", "-c",
		"apk add --no-cache file >/dev/null 2>&1 && file "+binaryPath).CombinedOutput()
	if err == nil {
		switch containerArch {
		case "x86_64":
			assert.Contains(t, string(fileOutput), "x86-64", "binary should be x86-64 for x86_64 container")
		case "aarch64":
			assert.Contains(t, string(fileOutput), "ARM aarch64", "binary should be ARM aarch64 for aarch64 container")
		}
	}
}
