package common

import (
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// IsDockerDesktop detects if we're running on Docker Desktop (Mac/Windows).
// Docker Desktop uses a VM, so Unix sockets through bind mounts don't work.
// Instead, Docker Desktop provides built-in SSH agent forwarding.
func IsDockerDesktop() bool {
	// Only relevant on macOS and Windows
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return false
	}

	// Use Docker CLI to get system info
	cmd := exec.Command("docker", "info", "--format", "{{.OperatingSystem}}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	return strings.Contains(string(output), "Docker Desktop")
}

// IsSSHAgentAvailable checks if an SSH agent is available on the host.
func IsSSHAgentAvailable() bool {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return false
	}

	// Check if socket exists and is accessible
	info, err := os.Stat(sock)
	if err != nil {
		return false
	}

	// Check if it's a socket
	if info.Mode()&os.ModeSocket == 0 {
		return false
	}

	// Try to connect to verify it's working
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return false
	}
	_ = conn.Close()

	return true
}
