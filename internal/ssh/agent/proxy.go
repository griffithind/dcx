package agent

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/griffithind/dcx/internal/util"
)

// GenerateSocketName generates a unique socket name for per-exec proxies.
func GenerateSocketName() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("agent-%s.sock", hex.EncodeToString(b))
}

// Proxy manages an SSH agent proxy socket.
type Proxy struct {
	envKey         string
	upstreamSocket string
	proxyDir       string
	socketPath     string
	listener       net.Listener
	mu             sync.Mutex
	running        bool
	done           chan struct{}
	wg             sync.WaitGroup
}

// NewProxy creates a new SSH agent proxy with default socket name.
func NewProxy(envKey string) (*Proxy, error) {
	return NewProxyWithSocket(envKey, "agent.sock")
}

// NewProxyWithSocket creates a new SSH agent proxy with a custom socket name.
// This allows multiple concurrent proxies in the same directory.
func NewProxyWithSocket(envKey, socketName string) (*Proxy, error) {
	// Get upstream socket
	upstreamSocket, err := GetUpstreamSocket()
	if err != nil {
		return nil, err
	}

	// Validate upstream socket
	if err := ValidateSocket(upstreamSocket); err != nil {
		return nil, fmt.Errorf("upstream SSH agent not accessible: %w", err)
	}

	// Determine proxy directory
	proxyDir, err := GetProxyDir(envKey)
	if err != nil {
		return nil, fmt.Errorf("failed to determine proxy directory: %w", err)
	}

	socketPath := filepath.Join(proxyDir, socketName)

	return &Proxy{
		envKey:         envKey,
		upstreamSocket: upstreamSocket,
		proxyDir:       proxyDir,
		socketPath:     socketPath,
		done:           make(chan struct{}),
	}, nil
}

// GetProxyDir returns the platform-specific proxy directory for SSH agent sockets.
func GetProxyDir(envKey string) (string, error) {
	var baseDir string

	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		baseDir = filepath.Join(home, "Library", "Caches", "dcx", envKey, "ssh-agent")

	default: // linux
		if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
			baseDir = filepath.Join(runtimeDir, "dcx", envKey, "ssh-agent")
		} else {
			// Fallback to /run/user/$UID
			baseDir = filepath.Join("/run", "user", fmt.Sprintf("%d", os.Getuid()), "dcx", envKey, "ssh-agent")
		}
	}

	return baseDir, nil
}

// Start starts the proxy server.
func (p *Proxy) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return nil
	}

	// Create proxy directory with restrictive permissions
	if err := util.EnsureDir(p.proxyDir, GetDirectoryMode()); err != nil {
		return fmt.Errorf("failed to create proxy directory: %w", err)
	}

	// Remove existing socket if present
	if util.Exists(p.socketPath) {
		if err := os.Remove(p.socketPath); err != nil {
			return fmt.Errorf("failed to remove existing socket: %w", err)
		}
	}

	// Set restrictive umask for socket creation
	restoreUmask := SetUmask(0077)
	defer restoreUmask()

	// Create listener
	listener, err := net.Listen("unix", p.socketPath)
	if err != nil {
		return fmt.Errorf("failed to create socket listener: %w", err)
	}

	p.listener = listener
	p.running = true
	p.done = make(chan struct{})

	// Start accepting connections
	p.wg.Add(1)
	go p.acceptLoop()

	return nil
}

// Stop stops the proxy server.
func (p *Proxy) Stop() error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	p.running = false
	close(p.done)
	p.mu.Unlock()

	// Close listener to unblock accept
	if p.listener != nil {
		p.listener.Close()
	}

	// Wait for goroutines to finish
	p.wg.Wait()

	// Cleanup socket file
	if util.Exists(p.socketPath) {
		os.Remove(p.socketPath)
	}

	return nil
}

// SocketPath returns the path to the proxy socket.
func (p *Proxy) SocketPath() string {
	return p.socketPath
}

// SocketName returns just the socket filename (e.g., "agent-abc123.sock").
func (p *Proxy) SocketName() string {
	return filepath.Base(p.socketPath)
}

// ProxyDir returns the proxy directory path.
func (p *Proxy) ProxyDir() string {
	return p.proxyDir
}

// IsRunning returns true if the proxy is running.
func (p *Proxy) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// acceptLoop accepts incoming connections and proxies them.
func (p *Proxy) acceptLoop() {
	defer p.wg.Done()

	for {
		select {
		case <-p.done:
			return
		default:
		}

		conn, err := p.listener.Accept()
		if err != nil {
			// Check if we're shutting down
			select {
			case <-p.done:
				return
			default:
				// Log error and continue
				util.Warn("SSH proxy accept error: %v", err)
				continue
			}
		}

		// Handle connection in goroutine
		p.wg.Add(1)
		go p.handleConnection(conn)
	}
}

// handleConnection proxies a single connection to the upstream agent.
func (p *Proxy) handleConnection(downstream net.Conn) {
	defer p.wg.Done()
	defer downstream.Close()

	// Connect to upstream agent (new connection per client)
	upstream, err := net.Dial("unix", p.upstreamSocket)
	if err != nil {
		util.Warn("SSH proxy failed to connect to upstream: %v", err)
		return
	}
	defer upstream.Close()

	// Bidirectional copy
	var wg sync.WaitGroup
	wg.Add(2)

	// Downstream -> Upstream
	go func() {
		defer wg.Done()
		io.Copy(upstream, downstream)
		// Signal upstream that we're done writing
		if uc, ok := upstream.(*net.UnixConn); ok {
			uc.CloseWrite()
		}
	}()

	// Upstream -> Downstream
	go func() {
		defer wg.Done()
		io.Copy(downstream, upstream)
		// Signal downstream that we're done writing
		if dc, ok := downstream.(*net.UnixConn); ok {
			dc.CloseWrite()
		}
	}()

	wg.Wait()
}
