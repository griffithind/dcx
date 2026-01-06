package ssh

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
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
	listener   net.Listener
	port       int
	done       chan struct{}
	wg         sync.WaitGroup
	agentSock  string

	// Container-side
	socketPath string
	clientPID  string // PID of the client process in container
}

// NewAgentProxy creates a new SSH agent proxy for the given container.
func NewAgentProxy(containerID, containerName string, uid, gid int) (*AgentProxy, error) {
	agentSock := os.Getenv("SSH_AUTH_SOCK")
	if agentSock == "" {
		return nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}

	// Validate the agent socket is accessible
	if err := ValidateSocket(agentSock); err != nil {
		return nil, fmt.Errorf("SSH agent not accessible: %w", err)
	}

	return &AgentProxy{
		containerID:   containerID,
		containerName: containerName,
		uid:           uid,
		gid:           gid,
		agentSock:     agentSock,
		done:          make(chan struct{}),
		socketPath:    fmt.Sprintf("/tmp/ssh-agent-%d.sock", uid),
	}, nil
}

// Start starts the agent proxy.
// Returns the socket path inside the container for SSH_AUTH_SOCK.
func (p *AgentProxy) Start() (string, error) {
	// Start TCP listener on host
	// On native Linux, bind to docker bridge to accept connections from containers.
	// On Docker Desktop (Mac/Windows), localhost works because of the VM networking.
	bindAddr := "127.0.0.1:0"
	if runtime.GOOS == "linux" && !IsDockerDesktop() {
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

	// Deploy dcx to container and start client
	if err := p.deployAndStartClient(); err != nil {
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
		p.listener.Close()
	}

	// Kill client in container
	if p.clientPID != "" {
		exec.Command("docker", "exec", p.containerName, "kill", p.clientPID).Run()
	}

	// Clean up socket and ready file in container
	exec.Command("docker", "exec", p.containerName, "rm", "-f", p.socketPath, p.socketPath+".ready").Run()

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
			tcpListener.SetDeadline(time.Now().Add(100 * time.Millisecond))
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
	defer tcpConn.Close()

	agentConn, err := net.Dial("unix", p.agentSock)
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

// deployAndStartClient deploys dcx to the container and starts the agent proxy client.
func (p *AgentProxy) deployAndStartClient() error {
	ctx := context.Background()
	binaryPath := GetContainerBinaryPath()

	// Deploy dcx binary to container (uses shared deployment code)
	if err := DeployToContainer(ctx, p.containerName, binaryPath); err != nil {
		return err
	}

	// Start client in background
	// On Docker Desktop, use host.docker.internal (built-in).
	// On native Linux, use the bridge gateway IP directly.
	var hostAddr string
	if runtime.GOOS == "linux" && !IsDockerDesktop() {
		hostAddr = fmt.Sprintf("%s:%d", getDockerBridgeIP(), p.port)
	} else {
		hostAddr = fmt.Sprintf("host.docker.internal:%d", p.port)
	}
	startCmd := exec.CommandContext(ctx, "docker", "exec", "-d", "--user", "root",
		p.containerName,
		binaryPath, "ssh-agent-proxy", "client",
		"--host", hostAddr,
		"--socket", p.socketPath,
		"--uid", strconv.Itoa(p.uid),
		"--gid", strconv.Itoa(p.gid),
	)

	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("failed to start client: %w", err)
	}

	// Get the PID of the client process
	pidCmd := exec.CommandContext(ctx, "docker", "exec", p.containerName,
		"sh", "-c", fmt.Sprintf("pgrep -f 'ssh-agent-proxy client.*%s'", p.socketPath))
	output, err := pidCmd.Output()
	if err == nil {
		p.clientPID = strings.TrimSpace(string(output))
	}

	return nil
}

// waitForSocket waits for the client socket to be ready.
func (p *AgentProxy) waitForSocket() error {
	readyFile := p.socketPath + ".ready"

	for i := 0; i < 50; i++ { // Wait up to 5 seconds
		checkCmd := exec.Command("docker", "exec", p.containerName, "test", "-f", readyFile)
		if err := checkCmd.Run(); err == nil {
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
func GetContainerUserIDs(containerName, user string) (int, int) {
	if user == "" {
		return 1000, 1000
	}

	// Run id command to get UID and GID
	ctx := context.Background()
	uidCmd := exec.CommandContext(ctx, "docker", "exec", containerName, "id", "-u", user)
	uidOut, err := uidCmd.Output()
	if err != nil {
		return 1000, 1000
	}

	gidCmd := exec.CommandContext(ctx, "docker", "exec", containerName, "id", "-g", user)
	gidOut, err := gidCmd.Output()
	if err != nil {
		return 1000, 1000
	}

	uid := 1000
	gid := 1000
	fmt.Sscanf(string(uidOut), "%d", &uid)
	fmt.Sscanf(string(gidOut), "%d", &gid)

	return uid, gid
}

// StartServerProcess starts the ssh-agent-proxy server as a subprocess.
// This is an alternative to running the server in-process.
// Returns the port number and a function to stop the server.
func StartServerProcess() (int, func(), error) {
	dcxPath, err := os.Executable()
	if err != nil {
		return 0, nil, err
	}

	cmd := exec.Command(dcxPath, "ssh-agent-proxy", "server", "--port", "0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, err
	}

	if err := cmd.Start(); err != nil {
		return 0, nil, err
	}

	// Read port from stdout
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() {
		cmd.Process.Kill()
		return 0, nil, fmt.Errorf("failed to read port from server")
	}

	port, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if err != nil {
		cmd.Process.Kill()
		return 0, nil, fmt.Errorf("invalid port: %w", err)
	}

	stop := func() {
		cmd.Process.Kill()
		cmd.Wait()
	}

	return port, stop, nil
}

// getDockerBridgeIP returns the gateway IP of the default Docker bridge network.
// This is the IP that containers use to reach the host on native Linux via host.docker.internal.
func getDockerBridgeIP() string {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "127.0.0.1"
	}
	defer cli.Close()

	network, err := cli.NetworkInspect(ctx, "bridge", network.InspectOptions{})
	if err != nil {
		return "127.0.0.1"
	}

	for _, config := range network.IPAM.Config {
		if config.Gateway != "" {
			return config.Gateway
		}
	}

	return "127.0.0.1"
}
