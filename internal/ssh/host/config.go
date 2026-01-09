// Package host provides SSH configuration management for the host system.
package host

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	sshConfigMarkerStart = "# DCX managed - "
	sshConfigMarkerEnd   = "# End DCX - "
)

// AddSSHConfig adds or updates an SSH config entry for a container.
// The entry is marked with comments so it can be identified and removed later.
func AddSSHConfig(hostName, containerName, user string) error {
	configPath := getSSHConfigPath()

	// Get the full path to dcx executable
	dcxPath, err := os.Executable()
	if err != nil {
		dcxPath = "dcx" // Fallback to PATH lookup
	}

	// Read existing config
	content, _ := os.ReadFile(configPath)

	// Remove existing entry if present
	content = removeSSHConfigEntry(content, containerName)

	// Build new entry
	entry := fmt.Sprintf(`%s%s
Host %s
  ProxyCommand %s ssh --stdio %s
  User %s
  ForwardAgent yes
  StrictHostKeyChecking no
  UserKnownHostsFile /dev/null
  LogLevel ERROR
%s%s

`, sshConfigMarkerStart, containerName, hostName, dcxPath, containerName, user, sshConfigMarkerEnd, containerName)

	// Append to config (or create new file)
	newContent := append(content, []byte(entry)...)

	// Ensure .ssh directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return fmt.Errorf("failed to create .ssh directory: %w", err)
	}

	return os.WriteFile(configPath, newContent, 0600)
}

// RemoveSSHConfig removes the SSH config entry for a container.
func RemoveSSHConfig(containerName string) error {
	configPath := getSSHConfigPath()
	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil // No config file, nothing to remove
	}

	newContent := removeSSHConfigEntry(content, containerName)

	// Only write if content changed
	if string(newContent) != string(content) {
		return os.WriteFile(configPath, newContent, 0600)
	}

	return nil
}

// HasSSHConfig checks if an SSH config entry exists for a container.
func HasSSHConfig(containerName string) bool {
	configPath := getSSHConfigPath()
	content, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	marker := sshConfigMarkerStart + containerName
	return strings.Contains(string(content), marker)
}

// getSSHConfigPath returns the path to the SSH config file.
func getSSHConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".ssh", "config")
}

// removeSSHConfigEntry removes a DCX-managed SSH config entry.
func removeSSHConfigEntry(content []byte, containerName string) []byte {
	lines := strings.Split(string(content), "\n")
	var result []string
	inManagedBlock := false

	for _, line := range lines {
		if strings.HasPrefix(line, sshConfigMarkerStart+containerName) {
			inManagedBlock = true
			continue
		}
		if strings.HasPrefix(line, sshConfigMarkerEnd+containerName) {
			inManagedBlock = false
			continue
		}
		if !inManagedBlock {
			result = append(result, line)
		}
	}

	// Remove trailing empty lines that might accumulate
	for len(result) > 0 && result[len(result)-1] == "" {
		result = result[:len(result)-1]
	}

	// Add a final newline
	if len(result) > 0 {
		return []byte(strings.Join(result, "\n") + "\n")
	}

	return []byte{}
}
