package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"syscall"

	"github.com/spf13/cobra"
)

var sshAgentProxyCmd = &cobra.Command{
	Use:    "ssh-agent-proxy",
	Short:  "SSH agent forwarding proxy (internal use)",
	Hidden: true, // Internal command, not shown in help
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
	sshProxyClientCmd.MarkFlagRequired("host")

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
	defer listener.Close()

	// Print port for parent to read
	port := listener.Addr().(*net.TCPAddr).Port
	fmt.Println(port)
	os.Stdout.Sync()

	for {
		conn, err := listener.Accept()
		if err != nil {
			continue
		}
		go proxyToSSHAgent(conn, sshProxyAgentSocket)
	}
}

func runSSHProxyClient(cmd *cobra.Command, args []string) error {
	// Remove existing socket
	os.Remove(sshProxyClientSocket)

	// Create Unix socket listener
	listener, err := net.Listen("unix", sshProxyClientSocket)
	if err != nil {
		return fmt.Errorf("failed to create socket: %w", err)
	}
	defer listener.Close()
	defer os.Remove(sshProxyClientSocket)

	// Set ownership so non-root user can access
	if err := syscall.Chown(sshProxyClientSocket, sshProxyClientUID, sshProxyClientGID); err != nil {
		return fmt.Errorf("failed to chown socket: %w", err)
	}

	// Signal ready by creating marker file
	readyFile := sshProxyClientSocket + ".ready"
	if err := os.WriteFile(readyFile, []byte("ready"), 0644); err != nil {
		return fmt.Errorf("failed to create ready file: %w", err)
	}
	defer os.Remove(readyFile)

	for {
		conn, err := listener.Accept()
		if err != nil {
			break
		}
		go proxyToHost(conn, sshProxyClientHost)
	}
	return nil
}

func proxyToSSHAgent(tcpConn net.Conn, agentPath string) {
	defer tcpConn.Close()

	agentConn, err := net.Dial("unix", agentPath)
	if err != nil {
		return
	}
	defer agentConn.Close()

	// Bidirectional copy
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(agentConn, tcpConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(tcpConn, agentConn)
		done <- struct{}{}
	}()
	<-done
}

func proxyToHost(unixConn net.Conn, hostAddr string) {
	defer unixConn.Close()

	tcpConn, err := net.Dial("tcp", hostAddr)
	if err != nil {
		return
	}
	defer tcpConn.Close()

	// Bidirectional copy
	done := make(chan struct{}, 2)
	go func() {
		io.Copy(tcpConn, unixConn)
		done <- struct{}{}
	}()
	go func() {
		io.Copy(unixConn, tcpConn)
		done <- struct{}{}
	}()
	<-done
}
