// Package server provides the CLI for dcx-agent, a minimal binary that runs
// inside containers and exposes an SSH surface for host-side dcx.
package agent

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
	case "provision":
		return runProvisionCmd(os.Args[2:])
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
  provision  Write secrets and session config, start the listener (run as root by dcx; request on stdin)

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
	shell := fs.String("shell", "", "Shell to use (default: $SHELL, else the user's login shell, else /bin/sh)")
	hostKey := fs.String("host-key", agentproto.HostKeyPath, "Path to persistent host key")
	authKeys := fs.String("authorized-keys", agentproto.AuthorizedKeysPath, "Primary authorized_keys file")
	allowCIDRs := fs.String("allow-cidrs", "", "Comma-separated CIDR list to accept in addition to loopback")

	if err := fs.Parse(args); err != nil {
		return err
	}

	shellPath := *shell
	if shellPath == "" {
		shellPath = sessionShell(os.Getenv("SHELL"), *userFlag, passwdFile)
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

// passwdFile is the passwd database sessionShell reads.
const passwdFile = "/etc/passwd"

// sessionShell picks the sessions' shell as devcontainers/cli does: the
// container's SHELL (the agent runs with the container's environment),
// else the user's login shell from the passwd database, else /bin/sh. A
// shell that is not executable, or one that refuses logins (nologin,
// false: system users such as nobody), is skipped.
func sessionShell(envShell, user, passwd string) string {
	for _, shell := range []string{envShell, loginShell(user, passwd)} {
		if base := filepath.Base(shell); base == "nologin" || base == "false" {
			continue
		}
		if info, err := os.Stat(shell); shell != "" && err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return shell
		}
	}
	return "/bin/sh"
}

// loginShell returns user's (a name or uid) shell from the passwd file, or
// "".
func loginShell(user, passwd string) string {
	data, err := os.ReadFile(passwd)
	if err != nil || user == "" {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && (fields[0] == user || fields[2] == user) {
			return fields[6]
		}
	}
	return ""
}
