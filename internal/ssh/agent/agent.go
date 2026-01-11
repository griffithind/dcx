// Package agent provides SSH agent detection and validation.
package agent

import (
	"fmt"
	"net"
	"os"

	"github.com/griffithind/dcx/internal/common"
)

// DockerDesktopSSHSocket is the path to Docker Desktop's built-in SSH agent socket.
const DockerDesktopSSHSocket = "/run/host-services/ssh-auth.sock"

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
	_ = conn.Close()

	return nil
}

// IsAvailable checks if an SSH agent is available.
func IsAvailable() bool {
	return common.IsSSHAgentAvailable()
}
