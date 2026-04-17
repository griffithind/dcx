// Package hostconfig manages the ~/.ssh/config block dcx writes for each
// workspace. The block is delimited by begin/end markers so subsequent
// dcx invocations can locate and replace it cleanly.
//
// Before the TCP transport, the block used `ProxyCommand dcx ssh --stdio …`
// which only worked for clients that shelled out to OpenSSH. This file now
// emits a plain `HostName/Port` block so any SSH-speaking client works
// without ProxyCommand plumbing.
package hostconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	dcxssh "github.com/griffithind/dcx/internal/ssh"
)

const (
	sshConfigMarkerStart = "# DCX managed - "
	sshConfigMarkerEnd   = "# End DCX - "
)

// withConfigLock executes a function while holding an exclusive lock on the SSH config.
// This prevents race conditions when multiple processes modify the config simultaneously.
func withConfigLock(fn func() error) error {
	lockPath := getSSHConfigPath() + ".dcx.lock"

	// Ensure .ssh directory exists before creating lock file
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		return fmt.Errorf("failed to create .ssh directory: %w", err)
	}

	// Open or create the lock file
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}
	defer func() { _ = lockFile.Close() }()

	// Acquire exclusive lock (blocks until lock is available)
	if err := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN) }()

	// Execute the function while holding the lock
	return fn()
}

// Entry captures everything needed to render one ~/.ssh/config block.
//
// HostName is almost always "127.0.0.1" — the only case that varies is
// network_mode: host, where the agent binds 48022 directly on the host.
type Entry struct {
	HostName       string
	ContainerName  string // used as the comment marker key for idempotent replace
	WorkspaceID    string // drives HostKeyAlias and the per-workspace known_hosts lookup
	User           string
	BindHost       string // HostName in the generated block (usually "127.0.0.1")
	Port           int
	KnownHostsPath string // usually ~/.dcx/known_hosts
}

// AddSSHConfig writes or replaces the ssh_config block for a container.
// Safe for concurrent use from multiple dcx invocations.
func AddSSHConfig(entry Entry) error {
	return withConfigLock(func() error {
		configPath := getSSHConfigPath()

		content, _ := os.ReadFile(configPath)
		content = removeSSHConfigEntry(content, entry.ContainerName)

		bindHost := entry.BindHost
		if bindHost == "" {
			bindHost = "127.0.0.1"
		}
		knownHosts := entry.KnownHostsPath
		if knownHosts == "" {
			// Best-effort; knownhosts.Path returns an absolute path we can use.
			if p, err := dcxssh.KnownHostsPath(); err == nil {
				knownHosts = p
			}
		}

		block := renderBlock(entry, bindHost, knownHosts)
		newContent := append(content, []byte(block)...)

		if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
			return fmt.Errorf("create .ssh dir: %w", err)
		}
		return os.WriteFile(configPath, newContent, 0600)
	})
}

// renderBlock formats the config stanza dcx writes.
func renderBlock(e Entry, bindHost, knownHosts string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s\n", sshConfigMarkerStart, e.ContainerName)
	fmt.Fprintf(&b, "Host %s\n", e.HostName)
	fmt.Fprintf(&b, "  HostName %s\n", bindHost)
	fmt.Fprintf(&b, "  Port %d\n", e.Port)
	if e.User != "" {
		fmt.Fprintf(&b, "  User %s\n", e.User)
	}
	if e.WorkspaceID != "" {
		fmt.Fprintf(&b, "  HostKeyAlias %s\n", dcxssh.HostKeyAlias(e.WorkspaceID))
	}
	if knownHosts != "" {
		fmt.Fprintf(&b, "  UserKnownHostsFile %s\n", knownHosts)
		fmt.Fprintln(&b, "  StrictHostKeyChecking yes")
	} else {
		// Fallback if we can't resolve a per-dcx known_hosts path. Keeps the
		// connection working at the cost of TOFU verification.
		fmt.Fprintln(&b, "  StrictHostKeyChecking no")
		fmt.Fprintln(&b, "  UserKnownHostsFile /dev/null")
	}
	// Advertise the dcx fallback identity so users without a standard
	// ~/.ssh/id_* (or an agent-loaded identity) can still connect via a
	// plain `ssh <host>` invocation. If the file is absent, OpenSSH silently
	// skips it, so this is safe to add unconditionally.
	if home, err := os.UserHomeDir(); err == nil {
		fmt.Fprintf(&b, "  IdentityFile %s\n", filepath.Join(home, ".dcx", "id_ed25519"))
	}
	fmt.Fprintln(&b, "  ForwardAgent yes")
	fmt.Fprintln(&b, "  IdentitiesOnly no")
	fmt.Fprintln(&b, "  LogLevel ERROR")
	fmt.Fprintf(&b, "%s%s\n\n", sshConfigMarkerEnd, e.ContainerName)
	return b.String()
}

// RemoveSSHConfig removes the SSH config entry for a container.
// Safe for concurrent use from multiple processes.
func RemoveSSHConfig(containerName string) error {
	return withConfigLock(func() error {
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
	})
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
