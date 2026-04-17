// Package ssh — knownhosts.go manages ~/.dcx/known_hosts, the per-workspace
// known_hosts file dcx generates for its SSH blocks.
//
// Using a dedicated file (not ~/.ssh/known_hosts) avoids polluting the user's
// global known_hosts with entries like [127.0.0.1]:53412 that rotate across
// container recreates. The generated ~/.ssh/config block references this
// file via UserKnownHostsFile and uses HostKeyAlias dcx-<workspaceID> so the
// known_hosts entry stays keyed by workspace identity rather than by port.
package ssh

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	gossh "golang.org/x/crypto/ssh"
)

// KnownHostsPath returns the absolute path to ~/.dcx/known_hosts.
func KnownHostsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".dcx", "known_hosts"), nil
}

// HostKeyAlias returns the stable alias used in both ~/.ssh/config's
// HostKeyAlias directive and as the first field of the known_hosts entry.
// The alias is derived purely from the workspace ID so it survives port
// rotation.
func HostKeyAlias(workspaceID string) string {
	return "dcx-" + workspaceID
}

// PinHostKey adds or updates the known_hosts entry for the workspace. The
// entry is keyed by HostKeyAlias(workspaceID), not by host:port, so entries
// remain valid even when the ephemeral host port changes.
//
// Existing entries for the same alias are replaced. The read-modify-write
// cycle is serialized via flock so parallel `dcx up` invocations don't
// clobber each other's entries.
func PinHostKey(workspaceID string, pub gossh.PublicKey) error {
	path, err := KnownHostsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create known_hosts dir: %w", err)
	}

	return withKnownHostsLock(path, func() error {
		alias := HostKeyAlias(workspaceID)
		entry := fmt.Sprintf("%s %s %s\n", alias, pub.Type(), base64.StdEncoding.EncodeToString(pub.Marshal()))

		content, readErr := os.ReadFile(path)
		if readErr != nil && !os.IsNotExist(readErr) {
			return fmt.Errorf("read known_hosts: %w", readErr)
		}
		content = removeAliasEntry(content, alias)
		content = append(content, entry...)

		return writeAtomic(path, content, 0600)
	})
}

// RemoveHost strips all known_hosts entries for the workspace. Used when the
// container is torn down (dcx down) so stale fingerprints don't linger.
func RemoveHost(workspaceID string) error {
	path, err := KnownHostsPath()
	if err != nil {
		return err
	}

	return withKnownHostsLock(path, func() error {
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				return nil
			}
			return fmt.Errorf("read known_hosts: %w", readErr)
		}

		alias := HostKeyAlias(workspaceID)
		newContent := removeAliasEntry(content, alias)
		if bytes.Equal(newContent, content) {
			return nil
		}
		return writeAtomic(path, newContent, 0600)
	})
}

// withKnownHostsLock holds an exclusive flock on a sibling .lock file so
// parallel PinHostKey/RemoveHost invocations cannot clobber each other's
// edits. The lock file is permanent (0-byte) so we don't race on its
// creation either.
func withKnownHostsLock(knownHostsPath string, fn func() error) error {
	lockPath := knownHostsPath + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0700); err != nil {
		return fmt.Errorf("create known_hosts lock dir: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open known_hosts lock: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquire known_hosts lock: %w", err)
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	return fn()
}

// HasHost reports whether the known_hosts file contains an entry for the
// workspace.
func HasHost(workspaceID string) (bool, error) {
	path, err := KnownHostsPath()
	if err != nil {
		return false, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	alias := HostKeyAlias(workspaceID)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if lineMatchesAlias(line, alias) {
			return true, nil
		}
	}
	return false, scanner.Err()
}

// removeAliasEntry returns the content with every line whose first field
// matches alias removed.
func removeAliasEntry(content []byte, alias string) []byte {
	if len(content) == 0 {
		return content
	}
	var out bytes.Buffer
	scanner := bufio.NewScanner(bytes.NewReader(content))
	// Default scanner buffer (64 KB) is plenty for known_hosts lines, but be
	// explicit in case a future line grows.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if lineMatchesAlias(line, alias) {
			continue
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}

// lineMatchesAlias reports whether a known_hosts line's first whitespace-
// separated field is exactly the alias. Comment lines (#) and blank lines
// never match. The hosts field can be comma-separated; we match only when
// alias is the sole host (which is how we write entries).
func lineMatchesAlias(line, alias string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	fields := strings.Fields(trimmed)
	if len(fields) < 3 {
		return false
	}
	hosts := strings.Split(fields[0], ",")
	for _, h := range hosts {
		if h == alias {
			return true
		}
	}
	return false
}

// writeAtomic writes data to path via a tempfile + rename.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

