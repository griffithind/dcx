// Package server provides the CLI for dcx-agent, a minimal binary that runs
// inside containers and exposes an SSH surface for host-side dcx.
package agent

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/griffithind/dcx/internal/agentproto"
)

// Execute runs the agent CLI.
func Execute() error {
	if len(os.Args) < 2 {
		printUsage()
		return fmt.Errorf("no command specified")
	}

	switch os.Args[1] {
	case "listen":
		return runListenCmd(os.Args[2:])
	case "ping":
		return runPingCmd(os.Args[2:])
	case "sftp-server":
		return runSFTPServerCmd(os.Args[2:])
	case "setup":
		return runSetupCmd(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `dcx-agent - DCX agent for container SSH functionality

Usage:
  dcx-agent <command> [flags]

Commands:
  listen  Run SSH server listening on a TCP address
  ping    Probe whether a listener is live (used by host for health checks)
  sftp-server  Serve SFTP on stdin/stdout (spawned by listen to drop privileges)
  setup   Prepare root-owned dirs, shell integration and log (run as root by dcx)

Use "dcx-agent <command> --help" for more information about a command.
`)
}

// runListenCmd starts the TCP SSH server. Runs as the container's
// long-lived agent process; returns when a signal is received or the
// listener errors.
func runListenCmd(args []string) error {
	fs := flag.NewFlagSet("listen", flag.ContinueOnError)
	addr := fs.String("addr", agentproto.ListenAddr("0.0.0.0"), "TCP address to bind (host:port)")
	userFlag := fs.String("user", "", "Default user for sessions (falls back to SSH client user)")
	workDir := fs.String("workdir", "/workspace", "Working directory")
	shell := fs.String("shell", "", "Shell to use (auto-detected if empty)")
	hostKey := fs.String("host-key", agentproto.HostKeyPath, "Path to persistent host key")
	authKeys := fs.String("authorized-keys", agentproto.AuthorizedKeysPath, "Primary authorized_keys file")
	allowCIDRs := fs.String("allow-cidrs", "", "Comma-separated CIDR list to accept in addition to loopback")

	if err := fs.Parse(args); err != nil {
		return err
	}

	shellPath := *shell
	if shellPath == "" {
		shellPath = detectShell()
	}

	gate, err := buildGate(*allowCIDRs)
	if err != nil {
		return fmt.Errorf("build gate: %w", err)
	}

	server, err := NewServer(Config{
		User:                *userFlag,
		Shell:               shellPath,
		WorkDir:             *workDir,
		HostKeyPath:         *hostKey,
		AuthorizedKeysPaths: []string{*authKeys},
		Gate:                gate,
		ReadyFile:           agentproto.ReadyFile,
	})
	if err != nil {
		return err
	}

	// Context that cancels on SIGTERM/SIGINT so Listen() can drain gracefully.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		cancel()
	}()

	fmt.Fprintf(os.Stderr, "dcx-agent: listening on %s (loopback+%s)\n", *addr, *allowCIDRs)
	return server.Listen(ctx, *addr)
}

// buildGate returns a Gate that accepts loopback, the container's default
// gateway (see below), plus any user-supplied --allow-cidrs.
//
// Why the default gateway is included: Docker publishes host ports by
// DNAT'ing to the container's bridge address and MASQUERADE'ing the
// source, so a packet originating on the host's 127.0.0.1 arrives inside
// the container with the source IP set to the docker bridge gateway
// (typically 172.17.0.1). The host-side -p 127.0.0.1:X:48022 is what
// actually enforces "loopback only" at the host level; the in-agent gate
// is defense-in-depth against WSL 0.0.0.0 binds and Docker regressions.
// Hardcoding loopback alone would reject every real connection.
func buildGate(extra string) (*Gate, error) {
	base := Loopback()
	if gw := defaultGateway(); gw != "" {
		// Narrow /32 so we only accept the gateway itself, not the whole
		// bridge subnet (containers on the same bridge would otherwise be
		// allowed).
		if extended, err := base.Extend([]string{gw + "/32"}); err == nil {
			base = extended
		}
	}
	extra = strings.TrimSpace(extra)
	if extra == "" {
		return base, nil
	}
	parts := strings.Split(extra, ",")
	return base.Extend(parts)
}

// defaultGateway reads /proc/net/route and returns the IPv4 default
// gateway as a dotted-quad string. Empty string on any error.
func defaultGateway() string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if i == 0 || line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// Columns: Iface Destination Gateway Flags RefCnt Use Metric Mask …
		// Destination == 00000000 means the default route.
		if fields[1] != "00000000" {
			continue
		}
		raw := fields[2]
		if len(raw) != 8 {
			continue
		}
		// Little-endian hex → dotted quad.
		var b [4]byte
		for j := 0; j < 4; j++ {
			var v int
			if _, err := fmt.Sscanf(raw[(3-j)*2:(3-j)*2+2], "%02x", &v); err != nil {
				return ""
			}
			b[j] = byte(v)
		}
		return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
	}
	return ""
}

// runPingCmd is used by host-side dcx to probe "is the listener up?"
// without needing ssh/nc/curl installed in the container image.
// Exit code 0 = reachable, non-zero = not.
func runPingCmd(args []string) error {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	addr := fs.String("addr", agentproto.LoopbackAddr, "Address to probe")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return pingAddr(*addr)
}

// runSetupCmd prepares the root-owned directories and files the listener
// uses. dcx runs it as root before each launch, because the listener
// itself runs as the container's default user, which is often not root:
//
//   - /var/lib/dcx and /run/dcx, root-owned (0755), so no container user can
//     plant symlinks or swap files the agent reads;
//   - the shell integration scripts;
//   - the agent log, owned by --log-owner (the listener's uid:gid) so the
//     listener can write it without the directory being writable.
func runSetupCmd(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	logOwner := fs.String("log-owner", "", "uid:gid that owns the agent log")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("setup must run as root")
	}
	for _, dir := range []string{agentproto.StateDir, agentproto.RunDir, agentproto.ShellIntegrationDir} {
		if err := ensureRootDir(dir); err != nil {
			return err
		}
	}
	if err := WriteShellIntegration(agentproto.ShellIntegrationDir); err != nil {
		return fmt.Errorf("write shell integration: %w", err)
	}

	uid, gid := 0, 0
	if *logOwner != "" {
		var err error
		if uid, gid, err = parseOwner(*logOwner); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(agentproto.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create agent log: %w", err)
	}
	defer func() { _ = f.Close() }()
	return f.Chown(uid, gid)
}

// ensureRootDir creates dir if needed and makes sure it is a real directory
// (not a symlink) owned by root with mode 0755.
func ensureRootDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if err := os.Chown(dir, 0, 0); err != nil {
		return err
	}
	return os.Chmod(dir, 0o755)
}

// parseOwner parses "uid:gid".
func parseOwner(s string) (int, int, error) {
	u, g, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0, fmt.Errorf("invalid owner %q: want uid:gid", s)
	}
	uid, err := strconv.Atoi(u)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid owner %q: %w", s, err)
	}
	gid, err := strconv.Atoi(g)
	if err != nil {
		return 0, 0, fmt.Errorf("invalid owner %q: %w", s, err)
	}
	return uid, gid, nil
}

// runSFTPServerCmd serves SFTP over stdin/stdout. The listener spawns it
// with the session user's credentials so file access is not performed as
// the agent's (typically root) identity.
func runSFTPServerCmd(args []string) error {
	fs := flag.NewFlagSet("sftp-server", flag.ContinueOnError)
	workDir := fs.String("workdir", "", "Initial working directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	serveSFTP(stdioConn{}, *workDir)
	return nil
}

// stdioConn adapts the process's stdin/stdout to an io.ReadWriteCloser.
type stdioConn struct{}

func (stdioConn) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdioConn) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdioConn) Close() error {
	_ = os.Stdin.Close()
	return os.Stdout.Close()
}

func detectShell() string {
	shells := []string{"/bin/bash", "/bin/zsh", "/bin/sh"}
	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}
	return "/bin/sh"
}
