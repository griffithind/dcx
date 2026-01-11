// Package selinux provides SELinux detection and configuration.
package selinux

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// Mode represents the SELinux mode.
type Mode string

const (
	// ModeEnforcing means SELinux is enforcing security policies.
	ModeEnforcing Mode = "enforcing"

	// ModePermissive means SELinux is logging but not enforcing.
	ModePermissive Mode = "permissive"

	// ModeDisabled means SELinux is disabled.
	ModeDisabled Mode = "disabled"

	// ModeUnknown means SELinux status could not be determined.
	ModeUnknown Mode = "unknown"
)

// String returns the string representation of the mode.
func (m Mode) String() string {
	return string(m)
}

// IsEnforcing returns true if SELinux is in enforcing mode.
func (m Mode) IsEnforcing() bool {
	return m == ModeEnforcing
}

// GetMode returns the current SELinux mode.
func GetMode() (Mode, error) {
	// SELinux only exists on Linux
	if runtime.GOOS != "linux" {
		return ModeDisabled, nil
	}

	// Try reading from /sys/fs/selinux/enforce
	mode, err := getModeFromSysfs()
	if err == nil {
		return mode, nil
	}

	// Fall back to getenforce command
	return getModeFromCommand()
}

// getModeFromSysfs reads SELinux mode from /sys/fs/selinux/enforce.
func getModeFromSysfs() (Mode, error) {
	// Check if SELinux filesystem exists
	if _, err := os.Stat("/sys/fs/selinux"); os.IsNotExist(err) {
		return ModeDisabled, nil
	}

	// Read enforce file
	data, err := os.ReadFile("/sys/fs/selinux/enforce")
	if err != nil {
		return ModeUnknown, err
	}

	content := strings.TrimSpace(string(data))
	switch content {
	case "1":
		return ModeEnforcing, nil
	case "0":
		return ModePermissive, nil
	default:
		return ModeUnknown, nil
	}
}

// getModeFromCommand uses the getenforce command to determine mode.
func getModeFromCommand() (Mode, error) {
	cmd := exec.Command("getenforce")
	output, err := cmd.Output()
	if err != nil {
		// Command not found or failed - assume SELinux not available
		return ModeDisabled, nil
	}

	content := strings.TrimSpace(strings.ToLower(string(output)))
	switch content {
	case "enforcing":
		return ModeEnforcing, nil
	case "permissive":
		return ModePermissive, nil
	case "disabled":
		return ModeDisabled, nil
	default:
		return ModeUnknown, nil
	}
}
