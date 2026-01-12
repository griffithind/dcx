// Package deploy provides dcx-agent binary deployment to containers.
package deploy

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	dcxembed "github.com/griffithind/dcx"
	"github.com/griffithind/dcx/internal/common"
)

// DeployToContainer deploys the dcx-agent binary to a container.
// It checks if the binary is already deployed and skips if so.
func DeployToContainer(ctx context.Context, containerName, binaryPath string) error {
	checkCmd := exec.CommandContext(ctx, "docker", "exec", containerName, "test", "-f", binaryPath)
	if err := checkCmd.Run(); err == nil {
		return nil
	}
	return copyBinaryToContainer(ctx, containerName, binaryPath)
}

func copyBinaryToContainer(ctx context.Context, containerName, binaryPath string) error {
	containerArch := getContainerArch(ctx, containerName)
	agentPath := getAgentBinaryPath(containerArch)
	needsCleanup := false

	if agentPath == "" {
		return fmt.Errorf("no agent binary available for architecture: %s", containerArch)
	}

	if strings.HasPrefix(agentPath, os.TempDir()) {
		needsCleanup = true
	}

	if needsCleanup {
		defer func() { _ = os.Remove(agentPath) }()
	}

	copyCmd := exec.CommandContext(ctx, "docker", "cp", agentPath, containerName+":"+binaryPath)
	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy agent to container: %w", err)
	}

	chmodCmd := exec.CommandContext(ctx, "docker", "exec", "--user", "root", containerName, "chmod", "+x", binaryPath)
	if err := chmodCmd.Run(); err != nil {
		return fmt.Errorf("failed to make agent executable: %w", err)
	}

	return nil
}

func getAgentBinaryPath(arch string) string {
	embeddedBinary, err := dcxembed.GetBinary(arch)
	if err != nil || len(embeddedBinary) == 0 {
		return ""
	}

	tmpFile, err := os.CreateTemp("", "dcx-agent-*")
	if err != nil {
		return ""
	}
	if _, err := tmpFile.Write(embeddedBinary); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return ""
	}
	_ = tmpFile.Close()
	return tmpFile.Name()
}

func getContainerArch(ctx context.Context, containerName string) string {
	cmd := exec.CommandContext(ctx, "docker", "exec", containerName, "uname", "-m")
	output, err := cmd.Output()
	if err != nil {
		return runtime.GOARCH
	}
	return strings.TrimSpace(string(output))
}

// GetContainerBinaryPath returns the path for dcx-agent binary in the container.
func GetContainerBinaryPath() string {
	return common.AgentBinaryPath
}

// PreDeployAgent deploys the dcx-agent binary to the specified container.
func PreDeployAgent(ctx context.Context, containerName string) error {
	return DeployToContainer(ctx, containerName, GetContainerBinaryPath())
}
