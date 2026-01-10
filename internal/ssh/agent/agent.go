// Package agent provides SSH agent detection and validation.
package agent

import (
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/griffithind/dcx/internal/common"
)

// DockerDesktopSSHSocket is the path to Docker Desktop's built-in SSH agent socket.
const DockerDesktopSSHSocket = "/run/host-services/ssh-auth.sock"

// IsDockerDesktop detects if we're running on Docker Desktop (Mac/Windows).
// Docker Desktop uses a VM, so Unix sockets through bind mounts don't work.
// Instead, Docker Desktop provides built-in SSH agent forwarding.
func IsDockerDesktop() bool {
	return common.IsDockerDesktop()
}

// GetUpstreamSocket returns the path to the host SSH agent socket.
func GetUpstreamSocket() (string, error) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return "", fmt.Errorf("SSH_AUTH_SOCK environment variable not set")
	}
	return sock, nil
}

// ValidateSocket checks if the given socket path is valid and accessible.
func ValidateSocket(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("socket not accessible: %w", err)
	}

	// Check if it's a socket
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("path is not a socket: %s", path)
	}

	// Try to connect to verify it's working
	conn, err := net.Dial("unix", path)
	if err != nil {
		return fmt.Errorf("cannot connect to socket: %w", err)
	}
	conn.Close()

	return nil
}

// IsAvailable checks if an SSH agent is available.
func IsAvailable() bool {
	return common.IsSSHAgentAvailable()
}

// GetSocketMode returns appropriate file permissions for socket files.
func GetSocketMode() os.FileMode {
	return 0600
}

// GetDirectoryMode returns appropriate file permissions for socket directories.
func GetDirectoryMode() os.FileMode {
	return 0700
}

// SetUmask sets a restrictive umask and returns a function to restore it.
func SetUmask(mask int) func() {
	old := syscall.Umask(mask)
	return func() {
		syscall.Umask(old)
	}
}
