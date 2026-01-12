package common

import (
	"net"
	"os"
)

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
