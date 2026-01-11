// Package agent provides the CLI for dcx-agent, a minimal binary that runs inside containers.
// It only includes commands needed for SSH server and agent proxy functionality.
package agent

import (
	"fmt"
	"net"
	"os"
	"syscall"

	"github.com/griffithind/dcx/internal/proxy"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "dcx-agent",
	Short: "DCX agent for container SSH functionality",
}

// Execute runs the agent CLI.
func Execute() error {
	return rootCmd.Execute()
}

// SSH Server command
var sshServerCmd = &cobra.Command{
	Use:   "ssh-server",
	Short: "Run SSH server in stdio mode",
	RunE:  runSSHServer,
}

var (
	sshServerUser    string
	sshServerWorkDir string
	sshServerShell   string
)

func init() {
	sshServerCmd.Flags().StringVar(&sshServerUser, "user", "", "User to run as")
	sshServerCmd.Flags().StringVar(&sshServerWorkDir, "workdir", "/workspace", "Working directory")
	sshServerCmd.Flags().StringVar(&sshServerShell, "shell", "", "Shell to use (auto-detected if empty)")
	rootCmd.AddCommand(sshServerCmd)
}

func runSSHServer(cmd *cobra.Command, args []string) error {
	shell := sshServerShell
	if shell == "" {
		shell = detectShell()
	}

	hostKeyPath := "/tmp/dcx-agent-ssh-hostkey"
	server, err := NewServer(sshServerUser, shell, sshServerWorkDir, hostKeyPath)
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

// SSH Agent Proxy commands
var sshAgentProxyCmd = &cobra.Command{
	Use:   "ssh-agent-proxy",
	Short: "SSH agent forwarding proxy",
}

var sshProxyServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Run TCP server that proxies to SSH agent",
	RunE:  runSSHProxyServer,
}

var sshProxyClientCmd = &cobra.Command{
	Use:   "client",
	Short: "Run client that creates Unix socket and proxies to TCP",
	RunE:  runSSHProxyClient,
}

var (
	sshProxyServerPort   int
	sshProxyAgentSocket  string
	sshProxyClientHost   string
	sshProxyClientSocket string
	sshProxyClientUID    int
	sshProxyClientGID    int
)

func init() {
	sshProxyServerCmd.Flags().IntVar(&sshProxyServerPort, "port", 0, "TCP port (0 = random)")
	sshProxyServerCmd.Flags().StringVar(&sshProxyAgentSocket, "agent", os.Getenv("SSH_AUTH_SOCK"), "SSH agent socket")

	sshProxyClientCmd.Flags().StringVar(&sshProxyClientHost, "host", "", "host:port to connect to")
	sshProxyClientCmd.Flags().StringVar(&sshProxyClientSocket, "socket", "/tmp/ssh-agent.sock", "Unix socket to create")
	sshProxyClientCmd.Flags().IntVar(&sshProxyClientUID, "uid", 1000, "Socket owner UID")
	sshProxyClientCmd.Flags().IntVar(&sshProxyClientGID, "gid", 1000, "Socket owner GID")
	if err := sshProxyClientCmd.MarkFlagRequired("host"); err != nil {
		panic(err) // programmer error: flag must exist
	}

	sshAgentProxyCmd.AddCommand(sshProxyServerCmd)
	sshAgentProxyCmd.AddCommand(sshProxyClientCmd)
	rootCmd.AddCommand(sshAgentProxyCmd)
}

func runSSHProxyServer(cmd *cobra.Command, args []string) error {
	if sshProxyAgentSocket == "" {
		return fmt.Errorf("SSH_AUTH_SOCK not set and --agent not provided")
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", sshProxyServerPort))
	if err != nil {
		return fmt.Errorf("failed to listen: %w", err)
	}
	defer func() { _ = listener.Close() }()

	port := listener.Addr().(*net.TCPAddr).Port
	fmt.Println(port)
	_ = os.Stdout.Sync()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go proxyToSSHAgent(conn, sshProxyAgentSocket)
	}
}

func runSSHProxyClient(cmd *cobra.Command, args []string) error {
	_ = os.Remove(sshProxyClientSocket)

	listener, err := net.Listen("unix", sshProxyClientSocket)
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}
	defer func() { _ = listener.Close() }()
	defer func() { _ = os.Remove(sshProxyClientSocket) }()

	if err := syscall.Chown(sshProxyClientSocket, sshProxyClientUID, sshProxyClientGID); err != nil {
		return fmt.Errorf("failed to chown socket: %w", err)
	}

	readyFile := sshProxyClientSocket + ".ready"
	if err := os.WriteFile(readyFile, []byte("ready"), 0644); err != nil {
		return fmt.Errorf("failed to create ready file: %w", err)
	}
	defer func() { _ = os.Remove(readyFile) }()

	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		go proxyToHost(conn, sshProxyClientHost)
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
