package common

import (
	"context"
	"net"
	"os"
	"runtime"
	"strings"

	"github.com/docker/docker/client"
)

// IsDockerDesktop detects if we're running on Docker Desktop (Mac/Windows).
// Docker Desktop uses a VM, so Unix sockets through bind mounts don't work.
// Instead, Docker Desktop provides built-in SSH agent forwarding.
func IsDockerDesktop() bool {
	// Only relevant on macOS and Windows
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		return false
	}

	// Use Docker SDK to get system info
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return false
	}
	defer cli.Close()

	info, err := cli.Info(ctx)
	if err != nil {
		return false
	}

	return strings.Contains(info.OperatingSystem, "Docker Desktop")
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
	conn.Close()

	return true
}
