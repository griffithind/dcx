package agent

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/proxy"
)

// AgentProxy manages SSH agent forwarding between host and container.
// It runs a TCP server on the host that proxies to the SSH agent,
// and deploys a client inside the container that creates a Unix socket.
type AgentProxy struct {
	containerID   string
	containerName string
	uid           int
	gid           int

	// Host-side
	listener     net.Listener
	port         int
	done         chan struct{}
	wg           sync.WaitGroup
	agentSock    string
	dockerClient *client.Client

	// Container-side
	socketPath string
}

// NewAgentProxy creates a new SSH agent proxy for the given container.
// The dcx binary must be pre-deployed to the container during 'up'.
func NewAgentProxy(containerID, containerName string, uid, gid int) (*AgentProxy, error) {
	agentSock := os.Getenv("SSH_AUTH_SOCK")
	if agentSock == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}

	// Validate the agent socket is accessible
	if err := ValidateSocket(agentSock); err != nil {
		return nil, fmt.Errorf("SSH agent not accessible: %w", err)
	}

	// Create Docker client for SDK-based exec operations
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	// Generate unique ID for this proxy instance to avoid conflicts with concurrent execs
	// Using process ID and timestamp ensures uniqueness
	uniqueID := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())

	return &AgentProxy{
		containerID:   containerID,
		containerName: containerName,
		uid:           uid,
		gid:           gid,
		agentSock:     agentSock,
		dockerClient:  dockerClient,
		done:          make(chan struct{}),
		socketPath:    fmt.Sprintf("/tmp/ssh-agent-%d-%s.sock", uid, uniqueID),
	}, nil
}

// Start starts the agent proxy.
// Returns the socket path inside the container for SSH_AUTH_SOCK.
func (p *AgentProxy) Start() (string, error) {
	// Start TCP listener on host
	// On native Linux, bind to docker bridge to accept connections from containers.
	// On Docker Desktop (Mac/Windows), localhost works because of the VM networking.
	bindAddr := "127.0.0.1:0"
	if runtime.GOOS == "linux" && !common.IsDockerDesktop() {
		// Use docker0 bridge IP (host-gateway) for native Linux
		bindAddr = getDockerBridgeIP() + ":0"
	}
	listener, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return "", fmt.Errorf("failed to start TCP listener: %w", err)
	}
	p.listener = listener
	p.port = listener.Addr().(*net.TCPAddr).Port

	// Start accepting connections
	p.wg.Add(1)
	go p.acceptLoop()

	// Start client in container (binary must be pre-deployed during 'up')
	if err := p.startClient(); err != nil {
		p.Stop()
		return "", fmt.Errorf("failed to start client in container: %w", err)
	}

	// Wait for socket to be ready
	if err := p.waitForSocket(); err != nil {
		p.Stop()
		return "", fmt.Errorf("client socket not ready: %w", err)
	}

	return p.socketPath, nil
}

// Stop stops the agent proxy and cleans up.
func (p *AgentProxy) Stop() {
	// Signal done to stop accept loop
	select {
	case <-p.done:
		// Already closed
	default:
		close(p.done)
	}

	// Close listener
	if p.listener != nil {
		_ = p.listener.Close()
	}

	ctx := context.Background()

	// Kill client process(es) in container using pkill with pattern matching
	// This is more reliable than capturing PIDs as it handles multiple processes
	// Run as root since the client was started as root
	pkillPattern := fmt.Sprintf("ssh-agent-proxy client.*%s", p.socketPath)
	_, _ = container.ExecSimple(ctx, p.dockerClient, p.containerID, []string{"pkill", "-f", pkillPattern}, "root")

	// Clean up socket and ready file in container
	_, _ = container.ExecSimple(ctx, p.dockerClient, p.containerID, []string{"rm", "-f", p.socketPath, p.socketPath + ".ready"}, "")

	// Close Docker client
	if p.dockerClient != nil {
		_ = p.dockerClient.Close()
	}

	// Wait for goroutines
	p.wg.Wait()
}

// acceptLoop accepts TCP connections and proxies them to the SSH agent.
func (p *AgentProxy) acceptLoop() {
	defer p.wg.Done()

	for {
		select {
		case <-p.done:
			return
		default:
		}

		// Set accept deadline to allow checking done channel
		if tcpListener, ok := p.listener.(*net.TCPListener); ok {
			_ = tcpListener.SetDeadline(time.Now().Add(100 * time.Millisecond))
		}

		conn, err := p.listener.Accept()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			select {
			case <-p.done:
				return
			default:
				continue
			}
		}

		p.wg.Add(1)
		go p.handleConnection(conn)
	}
}

// handleConnection proxies a TCP connection to the SSH agent.
func (p *AgentProxy) handleConnection(tcpConn net.Conn) {
	defer p.wg.Done()
	defer tcpConn.Close() //nolint:errcheck // best-effort cleanup

	agentConn, err := net.Dial("unix", p.agentSock)
	if err != nil {
		return
	}
	defer agentConn.Close() //nolint:errcheck // best-effort cleanup

	proxy.BidirectionalCopy(tcpConn, agentConn)
}

// startClient starts the agent proxy client in the container.
// The dcx binary must already be deployed to the container.
func (p *AgentProxy) startClient() error {
	ctx := context.Background()
	binaryPath := GetContainerBinaryPath()

	// Start client in background
	// On Docker Desktop, use host.docker.internal (built-in).
	// On native Linux, use the bridge gateway IP directly.
	var hostAddr string
	if runtime.GOOS == "linux" && !common.IsDockerDesktop() {
		hostAddr = fmt.Sprintf("%s:%d", getDockerBridgeIP(), p.port)
	} else {
		hostAddr = fmt.Sprintf("host.docker.internal:%d", p.port)
	}

	// Use SDK-based detached exec instead of CLI
	cmd := []string{
		binaryPath, "ssh-agent-proxy", "client",
		"--host", hostAddr,
		"--socket", p.socketPath,
		"--uid", strconv.Itoa(p.uid),
		"--gid", strconv.Itoa(p.gid),
	}

	if err := container.ExecDetached(ctx, p.dockerClient, p.containerID, cmd, "root"); err != nil {
		return fmt.Errorf("failed to start client: %w", err)
	}

	return nil
}

// waitForSocket waits for the client socket to be ready.
func (p *AgentProxy) waitForSocket() error {
	ctx := context.Background()
	readyFile := p.socketPath + ".ready"

	for i := 0; i < 50; i++ { // Wait up to 5 seconds
		// Use SDK-based exec to check if ready file exists
		exitCode, err := container.ExecSimple(ctx, p.dockerClient, p.containerID, []string{"test", "-f", readyFile}, "")
		if err == nil && exitCode == 0 {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout waiting for socket")
}

// SocketPath returns the socket path inside the container.
func (p *AgentProxy) SocketPath() string {
	return p.socketPath
}

// GetContainerUserIDs gets the UID and GID for a user in a container.
// If user is empty, returns default IDs (1000, 1000).
func GetContainerUserIDs(containerID, user string) (int, int) {
	if user == "" {
		return 1000, 1000
	}

	ctx := context.Background()

	// Create Docker client for SDK-based exec
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return 1000, 1000
	}
	defer dockerClient.Close() //nolint:errcheck // best-effort cleanup

	// Run id command to get UID
	uidOut, exitCode, err := container.ExecOutput(ctx, dockerClient, containerID, []string{"id", "-u", user}, "")
	if err != nil || exitCode != 0 {
		return 1000, 1000
	}

	// Run id command to get GID
	gidOut, exitCode, err := container.ExecOutput(ctx, dockerClient, containerID, []string{"id", "-g", user}, "")
	if err != nil || exitCode != 0 {
		return 1000, 1000
	}

	uid := 1000
	gid := 1000
	if _, err := fmt.Sscanf(strings.TrimSpace(uidOut), "%d", &uid); err != nil {
		return 1000, 1000
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(gidOut), "%d", &gid); err != nil {
		return 1000, 1000
	}

	return uid, gid
}

// GetContainerBinaryPath returns the path for dcx-agent binary in the container.
func GetContainerBinaryPath() string {
	return "/tmp/dcx-agent"
}

// getDockerBridgeIP returns the gateway IP of the default Docker bridge network.
// This is the IP that containers use to reach the host on native Linux via host.docker.internal.
func getDockerBridgeIP() string {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "127.0.0.1"
	}
	defer cli.Close() //nolint:errcheck // best-effort cleanup

	nw, err := cli.NetworkInspect(ctx, "bridge", network.InspectOptions{})
	if err != nil {
		return "127.0.0.1"
	}

	for _, config := range nw.IPAM.Config {
		if config.Gateway != "" {
			return config.Gateway
		}
	}

	return "127.0.0.1"
}
