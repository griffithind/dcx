// Package server provides the CLI for dcx-agent, a minimal binary that runs
// inside containers and exposes an SSH surface for host-side dcx.
package server

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/griffithind/dcx/internal/common"
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

Use "dcx-agent <command> --help" for more information about a command.
`)
}

// runListenCmd starts the TCP SSH server. Runs as the container's
// long-lived agent process; returns when a signal is received or the
// listener errors.
func runListenCmd(args []string) error {
	fs := flag.NewFlagSet("listen", flag.ContinueOnError)
	addr := fs.String("addr", "0.0.0.0:48022", "TCP address to bind (host:port)")
	userFlag := fs.String("user", "", "Default user for sessions (falls back to SSH client user)")
	workDir := fs.String("workdir", "/workspace", "Working directory")
	shell := fs.String("shell", "", "Shell to use (auto-detected if empty)")
	hostKey := fs.String("host-key", defaultHostKeyPath(), "Path to persistent host key")
	authKeys := fs.String("authorized-keys", defaultAuthorizedKeysPath(), "Primary authorized_keys file")
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
		ReadyFile:           DefaultReadyFilePath,
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

// defaultHostKeyPath returns the path where dcx mounts the per-workspace
// host key via the DCX secrets mechanism. Matches container.DCXSecretPath.
func defaultHostKeyPath() string {
	return filepath.Join(common.SecretsDir, "dcx", "ssh_host_ed25519_key")
}

// defaultAuthorizedKeysPath returns the path where dcx mounts the primary
// authorized_keys list. Matches container.DCXSecretPath.
func defaultAuthorizedKeysPath() string {
	return filepath.Join(common.SecretsDir, "dcx", "authorized_keys")
}

// runPingCmd is used by host-side dcx to probe "is the listener up?"
// without needing ssh/nc/curl installed in the container image.
// Exit code 0 = reachable, non-zero = not.
func runPingCmd(args []string) error {
	fs := flag.NewFlagSet("ping", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:48022", "Address to probe")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return pingAddr(*addr)
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
