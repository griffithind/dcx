//go:build e2e

package e2e

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	dcxembed "github.com/griffithind/dcx"
	"github.com/griffithind/dcx/internal/ssh/container"
	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// getContainerName extracts the container name from dcx status output.
// The status output has the format:
//
//	Primary Container
//	  Name:    container-name
func getContainerName(statusOutput string) string {
	for _, line := range strings.Split(statusOutput, "\n") {
		if strings.Contains(line, "Name:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

// TestInstallAgentDeploysToContainerE2E tests that the install agent can deploy
// the dcx binary to a container and run it.
func TestInstallAgentDeploysToContainerE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	if !dcxembed.HasBinaries() {
		t.Skip("Skipping: no embedded binaries available")
	}

	// Check if embeds are valid (not placeholders)
	binary, err := dcxembed.GetBinary("amd64")
	if err != nil || len(binary) < 1024*1024 {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	// Create a simple workspace and start container
	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the environment
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Get the container name from status
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
	containerName := getContainerName(stdout)
	require.NotEmpty(t, containerName, "should find container name")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Deploy the agent binary
	binaryPath := container.GetContainerBinaryPath()
	err = container.DeployToContainer(ctx, containerName, binaryPath)
	require.NoError(t, err, "DeployToContainer should succeed")

	// Verify binary exists in container
	checkCmd := exec.CommandContext(ctx, "docker", "exec", containerName, "test", "-f", binaryPath)
	err = checkCmd.Run()
	require.NoError(t, err, "binary should exist in container at %s", binaryPath)

	// Verify binary is executable
	checkCmd = exec.CommandContext(ctx, "docker", "exec", containerName, "test", "-x", binaryPath)
	err = checkCmd.Run()
	require.NoError(t, err, "binary should be executable")

	// Run the binary with --help
	helpCmd := exec.CommandContext(ctx, "docker", "exec", containerName, binaryPath, "--help")
	output, err := helpCmd.CombinedOutput()
	require.NoError(t, err, "binary --help should succeed: %s", string(output))
	assert.Contains(t, string(output), "dcx-agent", "help output should contain 'dcx-agent'")
}

// TestInstallAgentIdempotentE2E tests that deploying twice is idempotent.
func TestInstallAgentIdempotentE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	if !dcxembed.HasBinaries() {
		t.Skip("Skipping: no embedded binaries available")
	}

	binary, err := dcxembed.GetBinary("amd64")
	if err != nil || len(binary) < 1024*1024 {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
	containerName := getContainerName(stdout)
	require.NotEmpty(t, containerName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	binaryPath := container.GetContainerBinaryPath()

	// First deploy
	err = container.DeployToContainer(ctx, containerName, binaryPath)
	require.NoError(t, err, "first deploy should succeed")

	// Second deploy should also succeed (and be fast since binary exists)
	start := time.Now()
	err = container.DeployToContainer(ctx, containerName, binaryPath)
	require.NoError(t, err, "second deploy should succeed")
	elapsed := time.Since(start)

	// Second deploy should be fast (just a test -f check)
	assert.Less(t, elapsed, 2*time.Second, "idempotent deploy should be fast")
}

// TestInstallAgentPreDeployE2E tests the PreDeployAgent function.
func TestInstallAgentPreDeployE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	if !dcxembed.HasBinaries() {
		t.Skip("Skipping: no embedded binaries available")
	}

	binary, err := dcxembed.GetBinary("amd64")
	if err != nil || len(binary) < 1024*1024 {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
	containerName := getContainerName(stdout)
	require.NotEmpty(t, containerName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Use PreDeployAgent which is the main entry point
	err = container.PreDeployAgent(ctx, containerName)
	require.NoError(t, err, "PreDeployAgent should succeed")

	// Verify binary exists and runs
	binaryPath := container.GetContainerBinaryPath()
	helpCmd := exec.CommandContext(ctx, "docker", "exec", containerName, binaryPath, "--help")
	output, err := helpCmd.CombinedOutput()
	require.NoError(t, err, "deployed binary should run: %s", string(output))
	assert.Contains(t, string(output), "dcx-agent")
}

// TestInstallAgentArchitectureE2E tests that the correct architecture binary is deployed.
func TestInstallAgentArchitectureE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	if !dcxembed.HasBinaries() {
		t.Skip("Skipping: no embedded binaries available")
	}

	binary, err := dcxembed.GetBinary("amd64")
	if err != nil || len(binary) < 1024*1024 {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
	containerName := getContainerName(stdout)
	require.NotEmpty(t, containerName)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get container architecture
	archCmd := exec.CommandContext(ctx, "docker", "exec", containerName, "uname", "-m")
	archOutput, err := archCmd.Output()
	require.NoError(t, err)
	containerArch := strings.TrimSpace(string(archOutput))
	t.Logf("Container architecture: %s", containerArch)

	// Deploy
	binaryPath := container.GetContainerBinaryPath()
	err = container.DeployToContainer(ctx, containerName, binaryPath)
	require.NoError(t, err)

	// Verify binary can run (which confirms correct architecture)
	helpCmd := exec.CommandContext(ctx, "docker", "exec", containerName, binaryPath, "--help")
	output, err := helpCmd.CombinedOutput()
	require.NoError(t, err, "binary should run on %s architecture: %s", containerArch, string(output))

	// Check binary architecture using file command (if available)
	fileCmd := exec.CommandContext(ctx, "docker", "exec", containerName, "sh", "-c",
		"apk add --no-cache file >/dev/null 2>&1 && file "+binaryPath)
	fileOutput, err := fileCmd.CombinedOutput()
	if err == nil {
		outputStr := string(fileOutput)
		t.Logf("Binary file info: %s", outputStr)

		// Verify the architecture matches
		switch containerArch {
		case "x86_64":
			assert.Contains(t, outputStr, "x86-64", "binary should be x86-64 for x86_64 container")
		case "aarch64":
			assert.Contains(t, outputStr, "ARM aarch64", "binary should be ARM aarch64 for aarch64 container")
		}
	}
}
