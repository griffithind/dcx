// Package agent provides the CLI for dcx-agent, a minimal binary that runs inside containers.
// It only includes commands needed for SSH server and agent proxy functionality.
package agent

import (
	"flag"
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/griffithind/dcx/internal/proxy"
)

// Execute runs the agent CLI.
func Execute() error {
	if len(os.Args) < 2 {
		printUsage()
		return fmt.Errorf("no command specified")
	}

	switch os.Args[1] {
	case "ssh-server":
		return runSSHServerCmd(os.Args[2:])
	case "ssh-agent-proxy":
		if len(os.Args) < 3 {
			printProxyUsage()
			return fmt.Errorf("ssh-agent-proxy requires a subcommand")
		}
		switch os.Args[2] {
		case "server":
			return runProxyServerCmd(os.Args[3:])
		case "client":
			return runProxyClientCmd(os.Args[3:])
		default:
			printProxyUsage()
			return fmt.Errorf("unknown ssh-agent-proxy command: %s", os.Args[2])
		}
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
  ssh-server       Run SSH server in stdio mode
  ssh-agent-proxy  SSH agent forwarding proxy

Use "dcx-agent <command> --help" for more information about a command.
`)
}

func printProxyUsage() {
	fmt.Fprintf(os.Stderr, `dcx-agent ssh-agent-proxy - SSH agent forwarding proxy

Usage:
  dcx-agent ssh-agent-proxy <command> [flags]

Commands:
  server  Run TCP server that proxies to SSH agent
  client  Run client that creates Unix socket and proxies to TCP
`)
}

// SSH Server command
func runSSHServerCmd(args []string) error {
	fs := flag.NewFlagSet("ssh-server", flag.ContinueOnError)
	user := fs.String("user", "", "User to run as")
	workDir := fs.String("workdir", "/workspace", "Working directory")
	shell := fs.String("shell", "", "Shell to use (auto-detected if empty)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	shellPath := *shell
	if shellPath == "" {
		shellPath = detectShell()
	}

	hostKeyPath := "/tmp/dcx-agent-ssh-hostkey"
	server, err := NewServer(*user, shellPath, *workDir, hostKeyPath)
	if err != nil {
		return err
	}

	return server.Serve()
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

// SSH Agent Proxy server command
func runProxyServerCmd(args []string) error {
	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	port := fs.Int("port", 0, "TCP port (0 = random)")
	agentSocket := fs.String("agent", os.Getenv("SSH_AUTH_SOCK"), "SSH agent socket")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *agentSocket == "" {
		return fmt.Errorf("SSH_AUTH_SOCK not set and --agent not provided")
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *port))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer func() { _ = listener.Close() }()

	actualPort := listener.Addr().(*net.TCPAddr).Port
	fmt.Println(actualPort)
	_ = os.Stdout.Sync()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go proxyToSSHAgent(conn, *agentSocket)
	}
}

// SSH Agent Proxy client command
func runProxyClientCmd(args []string) error {
	fs := flag.NewFlagSet("client", flag.ContinueOnError)
	host := fs.String("host", "", "host:port to connect to (required)")
	socket := fs.String("socket", "/tmp/ssh-agent.sock", "Unix socket to create")
	uid := fs.Int("uid", 1000, "Socket owner UID")
	gid := fs.Int("gid", 1000, "Socket owner GID")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *host == "" {
		return fmt.Errorf("--host is required")
	}

	_ = os.Remove(*socket)

	listener, err := net.Listen("unix", *socket)
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}
	defer func() { _ = listener.Close() }()
	defer func() { _ = os.Remove(*socket) }()

	if err := syscall.Chown(*socket, *uid, *gid); err != nil {
		return fmt.Errorf("failed to chown socket: %w", err)
	}

	readyFile := *socket + ".ready"
	if err := os.WriteFile(readyFile, []byte("ready"), 0644); err != nil {
		return fmt.Errorf("failed to create ready file: %w", err)
	}
	defer func() { _ = os.Remove(readyFile) }()

	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		go proxyToHost(conn, *host)
	}
	return nil
}

// proxyToSSHAgent proxies a TCP connection to the SSH agent socket.
func proxyToSSHAgent(tcpConn net.Conn, agentPath string) {
	defer tcpConn.Close() //nolint:errcheck // best-effort cleanup

	agentConn, err := net.Dial("unix", agentPath)
	if err != nil {
		return
	}
	defer agentConn.Close() //nolint:errcheck // best-effort cleanup

	proxy.BidirectionalCopy(tcpConn, agentConn)
}

// proxyToHost proxies a Unix socket connection to a TCP host.
func proxyToHost(unixConn net.Conn, hostAddr string) {
	defer unixConn.Close() //nolint:errcheck // best-effort cleanup

	tcpConn, err := net.Dial("tcp", hostAddr)
	if err != nil {
		return
	}
	defer tcpConn.Close() //nolint:errcheck // best-effort cleanup

	proxy.BidirectionalCopy(unixConn, tcpConn)
}
