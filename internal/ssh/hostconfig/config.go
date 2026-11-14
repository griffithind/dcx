// Package hostconfig manages the SSH config block dcx writes for each
// workspace. Blocks live in ~/.ssh/dcx/config, which an `Include` line at
// the very top of ~/.ssh/config pulls in: OpenSSH uses the first value it
// finds for each option, so the include must precede the user's own
// `Host *` blocks or those would override dcx's settings. Each block is
// delimited by begin/end markers so dcx can locate and replace it.
//
// Blocks written to ~/.ssh/config by older dcx versions are moved into the
// include file the next time that workspace's block is written.
package hostconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/griffithind/dcx/internal/dcxhome"
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

// includeLine is the directive dcx adds to the top of ~/.ssh/config.
const includeLine = "Include ~/.ssh/dcx/config"

// SSHDirEnv overrides ~/.ssh for everything dcx writes (the main config,
// the include file, the Include line). The e2e tests set it so they never
// touch the developer's real SSH configuration; OpenSSH then needs
// `ssh -F $DCX_SSH_DIR/config` (see ConfigFileOverride).
const SSHDirEnv = "DCX_SSH_DIR"

// ConfigFileOverride returns the SSH config file to pass to `ssh -F` when
// SSHDirEnv is set, or "" when dcx uses the default ~/.ssh/config.
func ConfigFileOverride() string {
	if dir := os.Getenv(SSHDirEnv); dir != "" {
		return filepath.Join(dir, "config")
	}
	return ""
}

// wantedInclude is the Include directive for the include file: the short
// ~ form normally, an absolute path when SSHDirEnv relocates it.
func wantedInclude() string {
	if os.Getenv(SSHDirEnv) != "" {
		return absInclude()
	}
	return includeLine
}

// AddSSHConfig writes or replaces the ssh_config block for a container and
// returns the aliases it can be reached by. The block always answers to
// "<container>.dcx", which is unique per checkout; it also answers to
// entry.HostName ("<name>.dcx") unless another workspace's block already
// claims that alias. Safe for concurrent use from multiple dcx invocations.
func AddSSHConfig(entry Entry) ([]string, error) {
	// Every value lands in an ssh_config line: one with whitespace, quotes
	// or a newline could add directives of its own.
	for name, v := range map[string]string{"user": entry.User, "container name": entry.ContainerName, "host name": entry.HostName} {
		if !safeConfigToken(v) {
			return nil, fmt.Errorf("invalid SSH %s %q", name, v)
		}
	}
	var aliases []string
	err := withConfigLock(func() error {
		dcxPath := getDCXConfigPath()
		content, err := os.ReadFile(dcxPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read dcx ssh config: %w", err)
		}
		content = removeSSHConfigEntry(content, entry.ContainerName)

		aliases = []string{entry.ContainerName + ".dcx"}
		if entry.HostName != "" && entry.HostName != aliases[0] && !aliasTaken(content, entry.HostName) {
			aliases = append([]string{entry.HostName}, aliases...)
		}

		bindHost := entry.BindHost
		if bindHost == "" {
			bindHost = "127.0.0.1"
		}
		knownHosts := entry.KnownHostsPath
		if knownHosts == "" {
			if p, err := dcxssh.KnownHostsPath(); err == nil {
				knownHosts = p
			}
		}
		content = append(content, []byte(renderBlock(entry, aliases, bindHost, knownHosts))...)

		if err := os.MkdirAll(filepath.Dir(dcxPath), 0700); err != nil {
			return fmt.Errorf("create %s: %w", filepath.Dir(dcxPath), err)
		}
		if err := writeFileAtomic(dcxPath, content); err != nil {
			return err
		}
		return updateMainConfig(func(main []byte) []byte {
			// Move this workspace's legacy block out, then include ours.
			return ensureInclude(removeIfPresent(main, entry.ContainerName))
		})
	})
	return aliases, err
}

// Aliases returns the host aliases of a container's block, or nil when it
// has none.
func Aliases(containerName string) []string {
	for _, path := range []string{getDCXConfigPath(), getSSHConfigPath()} {
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if hosts := blockHosts(content, containerName); hosts != nil {
			return hosts
		}
	}
	return nil
}

// blockHosts returns the Host patterns of containerName's block.
func blockHosts(content []byte, containerName string) []string {
	start := sshConfigMarkerStart + containerName
	end := sshConfigMarkerEnd + containerName
	inBlock := false
	for _, line := range strings.Split(string(content), "\n") {
		trimmed := strings.TrimRight(line, " \t\r")
		switch {
		case trimmed == start:
			inBlock = true
		case trimmed == end:
			inBlock = false
		case inBlock:
			if fields := strings.Fields(trimmed); len(fields) > 1 && strings.EqualFold(fields[0], "Host") {
				return fields[1:]
			}
		}
	}
	return nil
}

// aliasTaken reports whether any block in content already lists alias as a
// Host pattern.
func aliasTaken(content []byte, alias string) bool {
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && strings.EqualFold(fields[0], "Host") {
			for _, h := range fields[1:] {
				if h == alias {
					return true
				}
			}
		}
	}
	return false
}

// removeIfPresent removes containerName's block from the user's own config,
// leaving the file byte-for-byte untouched when there is none.
func removeIfPresent(content []byte, containerName string) []byte {
	if blockHosts(content, containerName) == nil && !strings.Contains(string(content), sshConfigMarkerStart+containerName) {
		return content
	}
	return removeSSHConfigEntry(content, containerName)
}

// ensureInclude puts the dcx Include line at the top of the main config,
// unless it is already present.
func ensureInclude(main []byte) []byte {
	abs := absInclude()
	for _, line := range strings.Split(string(main), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == includeLine || trimmed == abs {
			return main
		}
	}
	header := "# Added by dcx: per-workspace hosts. Must stay above any Host block.\n" + wantedInclude() + "\n"
	if len(main) > 0 {
		header += "\n"
	}
	return append([]byte(header), main...)
}

// updateMainConfig rewrites ~/.ssh/config with fn, only when it changes.
func updateMainConfig(fn func([]byte) []byte) error {
	path := getSSHConfigPath()
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read ssh config: %w", err)
	}
	updated := fn(content)
	if string(updated) == string(content) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create .ssh dir: %w", err)
	}
	return writeFileAtomic(path, updated)
}

// renderBlock formats the config stanza dcx writes.
func renderBlock(e Entry, aliases []string, bindHost, knownHosts string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s\n", sshConfigMarkerStart, e.ContainerName)
	fmt.Fprintf(&b, "Host %s\n", strings.Join(aliases, " "))
	fmt.Fprintf(&b, "  HostName %s\n", bindHost)
	fmt.Fprintf(&b, "  Port %d\n", e.Port)
	if e.User != "" {
		fmt.Fprintf(&b, "  User %s\n", e.User)
	}
	if e.WorkspaceID != "" {
		fmt.Fprintf(&b, "  HostKeyAlias %s\n", dcxssh.HostKeyAlias(e.WorkspaceID))
	}
	if knownHosts != "" {
		fmt.Fprintf(&b, "  UserKnownHostsFile %s\n", configPath(knownHosts))
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
	if identity, err := dcxhome.Path("id_ed25519"); err == nil {
		fmt.Fprintf(&b, "  IdentityFile %s\n", configPath(identity))
	}
	fmt.Fprintln(&b, "  ForwardAgent yes")
	fmt.Fprintln(&b, "  IdentitiesOnly no")
	fmt.Fprintln(&b, "  LogLevel ERROR")
	fmt.Fprintf(&b, "%s%s\n\n", sshConfigMarkerEnd, e.ContainerName)
	return b.String()
}

// absInclude is the Include line naming the dcx config by absolute path,
// quoted when the path contains whitespace.
func absInclude() string {
	p := getDCXConfigPath()
	if strings.ContainsAny(p, " \t") {
		p = configPath(p)
	}
	return "Include " + p
}

// configPath renders a file path as one ssh_config argument: quoted (a
// home directory may contain spaces, which would split it into several
// arguments) with '%' doubled (OpenSSH expands %-tokens in these paths).
func configPath(p string) string {
	return `"` + strings.ReplaceAll(p, "%", "%%") + `"`
}

// safeConfigToken reports whether v can be written as a single ssh_config
// value: no whitespace, quotes, control characters or '#'.
func safeConfigToken(v string) bool {
	for _, r := range v {
		if r <= ' ' || r == 0x7f || r == '"' || r == '\'' || r == '#' || r == '\\' {
			return false
		}
	}
	return true
}

// RemoveSSHConfig removes the SSH config entry for a container, from the
// include file and from a legacy block in ~/.ssh/config. Safe for
// concurrent use from multiple processes.
func RemoveSSHConfig(containerName string) error {
	return withConfigLock(func() error {
		dcxPath := getDCXConfigPath()
		content, err := os.ReadFile(dcxPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("read dcx ssh config: %w", err)
		}
		if err == nil {
			if updated := removeSSHConfigEntry(content, containerName); string(updated) != string(content) {
				if err := writeFileAtomic(dcxPath, updated); err != nil {
					return err
				}
			}
		}
		if _, err := os.Stat(getSSHConfigPath()); os.IsNotExist(err) {
			return nil
		}
		return updateMainConfig(func(main []byte) []byte {
			return removeIfPresent(main, containerName)
		})
	})
}

// getDCXConfigPath returns the path of the include file holding dcx's blocks.
func getDCXConfigPath() string {
	return filepath.Join(filepath.Dir(getSSHConfigPath()), "dcx", "config")
}

// getSSHConfigPath returns the path to the SSH config file.
func getSSHConfigPath() string {
	if override := ConfigFileOverride(); override != "" {
		return override
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".ssh", "config")
}

// removeSSHConfigEntry removes every DCX-managed block for containerName.
//
// Markers must match a whole line: a prefix match would let removing "api"
// also hit the block for "api-v2". A start marker without a matching end
// marker is left untouched rather than deleting the rest of the file.
func removeSSHConfigEntry(content []byte, containerName string) []byte {
	lines := strings.Split(string(content), "\n")
	start := sshConfigMarkerStart + containerName
	end := sshConfigMarkerEnd + containerName
	isLine := func(line, marker string) bool {
		return strings.TrimRight(line, " \t\r") == marker
	}

	var result []string
	for i := 0; i < len(lines); i++ {
		if isLine(lines[i], start) {
			closing := -1
			for j := i + 1; j < len(lines); j++ {
				if isLine(lines[j], end) {
					closing = j
					break
				}
				if isLine(lines[j], start) {
					break
				}
			}
			if closing >= 0 {
				i = closing
				continue
			}
		}
		result = append(result, lines[i])
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

// writeFileAtomic replaces path with data via a temp file and rename, so a
// crash or full disk can never leave a truncated ~/.ssh/config. Symlinks
// (common with dotfile managers) are resolved so the link itself survives,
// and the existing file mode is preserved.
func writeFileAtomic(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		target = resolved
	}
	mode := os.FileMode(0600)
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".config.dcx-*")
	if err != nil {
		return fmt.Errorf("create temp ssh config: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp ssh config: %w", err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp ssh config: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("sync temp ssh config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp ssh config: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		cleanup()
		return fmt.Errorf("replace ssh config: %w", err)
	}
	return nil
}
