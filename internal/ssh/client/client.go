// Package client provides an SSH client that connects to dcx-agent's SSH server
// via docker exec stdio transport.
package client

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/devcontainer"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

// Client connects to dcx-agent's SSH server via docker exec.
type Client struct {
	ContainerName string
	User          string
	WorkDir       string
	AgentBinary   string // Path to dcx-agent in container
}

// ExecOptions configures command execution.
type ExecOptions struct {
	Command []string
	Env     []string
	TTY     bool
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

// stdioConn wraps docker exec's stdin/stdout as a net.Conn for SSH.
type stdioConn struct {
	stdin  io.WriteCloser
	stdout io.ReadCloser
	cmd    *exec.Cmd
	closed bool
	mu     sync.Mutex
}

func (c *stdioConn) Read(b []byte) (int, error) {
	return c.stdout.Read(b)
}

func (c *stdioConn) Write(b []byte) (int, error) {
	return c.stdin.Write(b)
}

func (c *stdioConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	_ = c.stdin.Close()
	_ = c.stdout.Close()
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_ = c.cmd.Wait()
	}
	return nil
}

func (c *stdioConn) LocalAddr() net.Addr {
	return &net.UnixAddr{Name: "stdio", Net: "unix"}
}

func (c *stdioConn) RemoteAddr() net.Addr {
	return &net.UnixAddr{Name: "stdio", Net: "unix"}
}

func (c *stdioConn) SetDeadline(t time.Time) error {
	return nil
}

func (c *stdioConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (c *stdioConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// Connect establishes an SSH connection via stdio transport.
func (c *Client) Connect(ctx context.Context) (*ssh.Client, *stdioConn, error) {
	// Build docker exec command for dcx-agent ssh-server
	// Use -u to run as the target user in Docker
	args := []string{"exec", "-i"}
	if c.User != "" {
		args = append(args, "-u", c.User)
	}
	args = append(args, c.ContainerName, c.AgentBinary, "ssh-server")
	if c.User != "" {
		args = append(args, "--user", c.User)
	}
	if c.WorkDir != "" {
		args = append(args, "--workdir", c.WorkDir)
	}

	cmd := exec.CommandContext(ctx, "docker", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get stdout pipe: %w", err)
	}

	// Start the docker exec process
	if err := cmd.Start(); err != nil {
		return nil, nil, fmt.Errorf("failed to start docker exec: %w", err)
	}

	// Create SSH client config
	config := &ssh.ClientConfig{
		User:            c.User,
		Auth:            c.getAuthMethods(),
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	// Create stdio connection
	conn := &stdioConn{stdin: stdin, stdout: stdout, cmd: cmd}

	// Establish SSH connection
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, "stdio", config)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("SSH handshake failed: %w", err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)

	// Handle agent forwarding requests from server.
	// When server's ForwardAgentConnections opens auth-agent channels, we proxy to local agent.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		go func() {
			agentChans := client.HandleChannelOpen("auth-agent@openssh.com")
			for newChannel := range agentChans {
				channel, reqs, err := newChannel.Accept()
				if err != nil {
					continue
				}
				go ssh.DiscardRequests(reqs)

				// Connect to local agent
				agentConn, err := net.Dial("unix", sock)
				if err != nil {
					_ = channel.Close()
					continue
				}

				// Proxy bidirectionally (ssh.Channel doesn't implement net.Conn)
				go func(ch ssh.Channel, conn net.Conn) {
					defer func() { _ = ch.Close() }()
					defer func() { _ = conn.Close() }()
					go func() { _, _ = io.Copy(ch, conn) }()
					_, _ = io.Copy(conn, ch)
				}(channel, agentConn)
			}
		}()
	}

	return client, conn, nil
}

// getAuthMethods returns SSH authentication methods.
// The server accepts any auth, but we still need to provide methods.
func (c *Client) getAuthMethods() []ssh.AuthMethod {
	methods := []ssh.AuthMethod{
		ssh.Password(""), // Empty password (server accepts any)
	}

	// Try to use SSH agent if available
	if agentConn := c.getAgentConn(); agentConn != nil {
		methods = append([]ssh.AuthMethod{
			ssh.PublicKeysCallback(agent.NewClient(agentConn).Signers),
		}, methods...)
	}

	return methods
}

// getAgentConn returns a connection to the SSH agent if available.
func (c *Client) getAgentConn() net.Conn {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil
	}
	return conn
}

// Exec runs a command via SSH.
func (c *Client) Exec(ctx context.Context, opts ExecOptions) (int, error) {
	client, conn, err := c.Connect(ctx)
	if err != nil {
		return -1, err
	}
	defer func() { _ = client.Close() }()
	defer func() { _ = conn.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("failed to create session: %w", err)
	}
	defer func() { _ = session.Close() }()

	// Request agent forwarding if SSH agent is available
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		// Non-fatal: agent forwarding is optional
		_ = agent.RequestAgentForwarding(session)
	}

	// Handle TTY mode
	if opts.TTY {
		// Put terminal in raw mode
		fd := int(os.Stdin.Fd())
		oldState, err := term.MakeRaw(fd)
		if err == nil {
			defer func() { _ = term.Restore(fd, oldState) }()
		}

		// Get terminal size
		w, h, _ := term.GetSize(fd)
		if w == 0 {
			w = 80
		}
		if h == 0 {
			h = 24
		}

		// Request PTY
		termType := os.Getenv("TERM")
		if termType == "" {
			termType = "xterm-256color"
		}
		modes := ssh.TerminalModes{
			ssh.ECHO:          1,
			ssh.TTY_OP_ISPEED: 14400,
			ssh.TTY_OP_OSPEED: 14400,
		}
		if err := session.RequestPty(termType, h, w, modes); err != nil {
			return -1, fmt.Errorf("failed to request PTY: %w", err)
		}

		// Handle window resize
		go c.handleWindowResize(ctx, session, fd)
	}

	// Set environment variables
	for _, env := range opts.Env {
		if idx := strings.Index(env, "="); idx > 0 {
			key := env[:idx]
			value := env[idx+1:]
			// Ignore errors - some servers don't support SetEnv
			_ = session.Setenv(key, value)
		}
	}

	// Wire I/O
	session.Stdin = opts.Stdin
	session.Stdout = opts.Stdout
	session.Stderr = opts.Stderr

	// Run command or shell
	if len(opts.Command) == 0 {
		// Interactive shell
		err = session.Shell()
		if err != nil {
			return -1, fmt.Errorf("failed to start shell: %w", err)
		}
		err = session.Wait()
	} else {
		// Run command - properly quote arguments for shell execution
		cmdStr := joinCommandForShell(opts.Command)
		err = session.Run(cmdStr)
	}

	// Extract exit code
	if err != nil {
		if exitErr, ok := err.(*ssh.ExitError); ok {
			return exitErr.ExitStatus(), nil
		}
		if err == io.EOF {
			return 0, nil
		}
		return -1, err
	}
	return 0, nil
}

// handleWindowResize monitors terminal size changes and updates the SSH session.
func (c *Client) handleWindowResize(ctx context.Context, session *ssh.Session, fd int) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigCh:
			w, h, err := term.GetSize(fd)
			if err == nil && w > 0 && h > 0 {
				_ = session.WindowChange(h, w)
			}
		}
	}
}

// ContainerExecOptions configures unified container command execution.
// This is the high-level API used by CLI commands and lifecycle hooks.
type ContainerExecOptions struct {
	ContainerName string
	Config        *devcontainer.DevContainerConfig
	WorkspacePath string
	Command       []string  // nil = interactive shell
	Env           []string  // additional env vars (appended to defaults)
	Stdin         io.Reader // defaults to os.Stdin
	Stdout        io.Writer // defaults to os.Stdout
	Stderr        io.Writer // defaults to os.Stderr
	TTY           *bool     // nil = auto-detect from stdin
}

// ExecInContainer is the unified execution path for all container commands.
// Used by exec, shell, run CLI commands and lifecycle hooks.
func ExecInContainer(ctx context.Context, opts ContainerExecOptions) (int, error) {
	// Resolve user and workdir from config
	user, workDir := resolveUserAndWorkDir(opts.Config, opts.WorkspacePath)

	// Build base environment
	env := buildExecEnvironment(user, opts.Config)
	env = append(env, opts.Env...) // Caller env takes precedence

	// Determine TTY mode
	tty := false
	if opts.TTY != nil {
		tty = *opts.TTY
	} else if f, ok := opts.Stdin.(*os.File); ok {
		tty = term.IsTerminal(int(f.Fd()))
	} else if opts.Stdin == nil {
		tty = term.IsTerminal(int(os.Stdin.Fd()))
	}

	// Set I/O defaults
	stdin, stdout, stderr := opts.Stdin, opts.Stdout, opts.Stderr
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	// Execute via SSH client
	c := &Client{
		ContainerName: opts.ContainerName,
		User:          user,
		WorkDir:       workDir,
		AgentBinary:   common.AgentBinaryPath,
	}
	return c.Exec(ctx, ExecOptions{
		Command: opts.Command,
		Env:     env,
		TTY:     tty,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
	})
}

// resolveUserAndWorkDir determines the user and working directory for container execution.
// It uses values from the devcontainer config if available, with sensible defaults.
func resolveUserAndWorkDir(cfg *devcontainer.DevContainerConfig, workspacePath string) (user, workDir string) {
	if cfg != nil {
		user = cfg.RemoteUser
		if user == "" {
			user = cfg.ContainerUser
		}
		if user != "" {
			user = devcontainer.Substitute(user, &devcontainer.SubstitutionContext{
				LocalWorkspaceFolder: workspacePath,
			})
		}
		workDir = devcontainer.DetermineContainerWorkspaceFolder(cfg, workspacePath)
	}

	// Defaults
	if user == "" {
		user = "root"
	}
	if workDir == "" {
		workDir = "/workspace"
	}

	return user, workDir
}

// buildExecEnvironment creates the base environment for container execution.
func buildExecEnvironment(user string, cfg *devcontainer.DevContainerConfig) []string {
	env := []string{
		"USER=" + user,
		"HOME=" + common.GetDefaultHomeDir(user),
	}

	// Add remoteEnv from config (per spec, applies to all exec operations)
	if cfg != nil {
		for k, v := range cfg.RemoteEnv {
			env = append(env, k+"="+v)
		}
	}

	return env
}

// shellUnsafeChars matches characters that need quoting in shell arguments.
var shellUnsafeChars = regexp.MustCompile(`[^\w@%+=:,./-]`)

// joinCommandForShell joins command arguments with proper shell quoting.
// Arguments containing spaces, quotes, or other special characters are quoted.
func joinCommandForShell(args []string) string {
	if len(args) == 0 {
		return ""
	}

	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteForShell(arg)
	}
	return strings.Join(quoted, " ")
}

// quoteForShell quotes a string for safe use as a shell argument.
// Uses single quotes for strings with special characters, with proper escaping.
func quoteForShell(s string) string {
	if s == "" {
		return "''"
	}

	// If string contains no special characters, no quoting needed
	if !shellUnsafeChars.MatchString(s) {
		return s
	}

	// Use single quotes, escaping any single quotes within the string
	// 'foo' -> 'foo'
	// foo's -> 'foo'"'"'s'
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
