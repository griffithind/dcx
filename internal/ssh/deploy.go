package ssh

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// DeployToContainer deploys the dcx binary to a container.
// It checks if the correct version is already deployed and skips if so.
func DeployToContainer(ctx context.Context, containerName, binaryPath string) error {
	// Check if correct version of dcx is already in container
	checkCmd := exec.CommandContext(ctx, "docker", "exec", containerName, "test", "-f", binaryPath)
	if err := checkCmd.Run(); err == nil {
		// Binary already exists
		return nil
	}

	// Need to copy dcx to container
	return copyBinaryToContainer(ctx, containerName, binaryPath)
}

// copyBinaryToContainer copies the dcx binary to the container.
func copyBinaryToContainer(ctx context.Context, containerName, binaryPath string) error {
	// Detect container architecture
	containerArch := getContainerArch(ctx, containerName)

	// Try to get a Linux binary for the container's architecture
	dcxPath := getLinuxBinaryPath(containerArch)
	needsCleanup := false

	if dcxPath == "" {
		// Fall back to current executable (works when already on Linux)
		var err error
		dcxPath, err = os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
	} else if strings.HasPrefix(dcxPath, os.TempDir()) {
		// If it's a temp file (from embedded binary), clean it up after
		needsCleanup = true
	}

	if needsCleanup {
		defer os.Remove(dcxPath)
	}

	// Copy to container
	copyCmd := exec.CommandContext(ctx, "docker", "cp", dcxPath, containerName+":"+binaryPath)
	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy dcx to container: %w", err)
	}

	// Make executable (run as root to avoid permission issues)
	chmodCmd := exec.CommandContext(ctx, "docker", "exec", "--user", "root", containerName, "chmod", "+x", binaryPath)
	if err := chmodCmd.Run(); err != nil {
		return fmt.Errorf("failed to make dcx executable: %w", err)
	}

	return nil
}

// getLinuxBinaryPath returns the path to a Linux binary for the given architecture.
// Returns empty string if not available.
func getLinuxBinaryPath(arch string) string {
	// Check for embedded binaries first (not available on Linux builds)
	if runtime.GOOS != "linux" {
		var embeddedBinary []byte
		switch arch {
		case "amd64", "x86_64":
			embeddedBinary = dcxLinuxAmd64
		case "arm64", "aarch64":
			embeddedBinary = dcxLinuxArm64
		}

		if len(embeddedBinary) > 0 {
			// Write embedded binary to temp file
			tmpFile, err := os.CreateTemp("", "dcx-linux-*")
			if err != nil {
				return ""
			}
			if _, err := tmpFile.Write(embeddedBinary); err != nil {
				tmpFile.Close()
				os.Remove(tmpFile.Name())
				return ""
			}
			tmpFile.Close()
			return tmpFile.Name()
		}
	}

	// Check for pre-built binaries next to current executable
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exeDir := filepath.Dir(exe)

	var binaryName string
	switch arch {
	case "amd64", "x86_64":
		binaryName = "dcx-linux-amd64"
	case "arm64", "aarch64":
		binaryName = "dcx-linux-arm64"
	default:
		return ""
	}

	linuxBinaryPath := filepath.Join(exeDir, binaryName)
	if _, err := os.Stat(linuxBinaryPath); err == nil {
		return linuxBinaryPath
	}

	return ""
}

// getContainerArch returns the architecture of the container.
func getContainerArch(ctx context.Context, containerName string) string {
	cmd := exec.CommandContext(ctx, "docker", "exec", containerName, "uname", "-m")
	output, err := cmd.Output()
	if err != nil {
		// Fall back to host architecture
		return runtime.GOARCH
	}
	return strings.TrimSpace(string(output))
}

// GetContainerBinaryPath returns the path for dcx binary in the container.
func GetContainerBinaryPath() string {
	return "/tmp/dcx"
}

// PreDeployAgent deploys the dcx agent binary to the specified container.
// This should be called once during 'up' before lifecycle hooks run.
// Returns nil if the binary is already present (idempotent).
func PreDeployAgent(ctx context.Context, containerName string) error {
	binaryPath := GetContainerBinaryPath()
	return DeployToContainer(ctx, containerName, binaryPath)
}
