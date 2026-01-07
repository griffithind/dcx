package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// Server is an SSH server that runs inside a container.
// It only supports stdio mode (stdin/stdout transport).
type Server struct {
	server      *ssh.Server
	user        string
	shell       string
	workDir     string
	hostKeyPath string
}

// NewServer creates a new SSH server.
func NewServer(username, shell, workDir, hostKeyPath string) (*Server, error) {
	s := &Server{
		user:        username,
		shell:       shell,
		workDir:     workDir,
		hostKeyPath: hostKeyPath,
	}

	server := &ssh.Server{
		Handler: s.sessionHandler,
		SubsystemHandlers: map[string]ssh.SubsystemHandler{
			"sftp": s.sftpHandler,
		},
		// Allow port forwarding (needed for VS Code server communication)
		LocalPortForwardingCallback: func(ctx ssh.Context, dhost string, dport uint32) bool {
			return true
		},
		ReversePortForwardingCallback: func(ctx ssh.Context, host string, port uint32) bool {
			return true
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"direct-tcpip": ssh.DirectTCPIPHandler,
			"session":      ssh.DefaultSessionHandler,
		},
		RequestHandlers: map[string]ssh.RequestHandler{
			"tcpip-forward":        (&ssh.ForwardedTCPHandler{}).HandleSSHRequest,
			"cancel-tcpip-forward": (&ssh.ForwardedTCPHandler{}).HandleSSHRequest,
		},
	}

	// No authentication required (localhost trusted via docker exec)
	server.SetOption(ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
		return true // Accept any key
	}))
	server.SetOption(ssh.PasswordAuth(func(ctx ssh.Context, pass string) bool {
		return true // Accept any password
	}))

	// Load or generate host key
	if err := s.setupHostKey(server); err != nil {
		return nil, err
	}

	s.server = server
	return s, nil
}

// Serve handles a single SSH connection via stdin/stdout (stdio mode only).
func (s *Server) Serve() error {
	// Create a net.Conn wrapper around stdin/stdout
	conn := &stdioConn{
		Reader: os.Stdin,
		Writer: os.Stdout,
	}

	// Handle single connection
	s.server.HandleConn(conn)
	return nil
}

// sessionHandler handles SSH session requests.
func (s *Server) sessionHandler(sess ssh.Session) {
	// Setup agent forwarding if requested
	var agentSock string
	if ssh.AgentRequested(sess) {
		ln, err := ssh.NewAgentListener()
		if err == nil {
			defer ln.Close()
			go ssh.ForwardAgentConnections(ln, sess)
			agentSock = ln.Addr().String()
		}
	}

	ptyReq, winCh, isPty := sess.Pty()

	cmd := s.buildCommand(sess, isPty)
	cmd.Env = append(cmd.Env, sess.Environ()...)
	if agentSock != "" {
		cmd.Env = append(cmd.Env, "SSH_AUTH_SOCK="+agentSock)
	}

	if isPty {
		cmd.Env = append(cmd.Env, "TERM="+ptyReq.Term)
		s.runWithPTY(sess, cmd, ptyReq, winCh)
	} else {
		s.runWithoutPTY(sess, cmd)
	}
}

// buildCommand creates an exec.Cmd for the session.
func (s *Server) buildCommand(sess ssh.Session, isPty bool) *exec.Cmd {
	var cmd *exec.Cmd

	// Check if there's a specific command to run.
	// Use RawCommand() to get the exact command string as sent by the client,
	// preserving quotes and special characters. This matches OpenSSH behavior
	// where the command is passed as-is to the shell via -c.
	rawCmd := sess.RawCommand()
	if rawCmd != "" {
		cmd = exec.Command(s.shell, "-c", rawCmd)
	} else {
		// Start a login shell
		cmd = exec.Command(s.shell)
		if isPty {
			// Add -l flag for login shell
			cmd.Args = append(cmd.Args, "-l")
		}
	}

	cmd.Dir = s.workDir
	cmd.Env = s.buildEnvironment()

	return cmd
}

// buildEnvironment creates the environment for the shell.
func (s *Server) buildEnvironment() []string {
	env := os.Environ()

	// Set user-related environment variables
	env = append(env, "USER="+s.user)
	env = append(env, "LOGNAME="+s.user)

	// Try to get home directory
	if u, err := user.Lookup(s.user); err == nil {
		env = append(env, "HOME="+u.HomeDir)
	} else {
		env = append(env, "HOME=/home/"+s.user)
	}

	// Set shell
	env = append(env, "SHELL="+s.shell)

	return env
}

// runWithPTY runs a command with a pseudo-terminal.
func (s *Server) runWithPTY(sess ssh.Session, cmd *exec.Cmd, ptyReq ssh.Pty, winCh <-chan ssh.Window) {
	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start PTY: %v\n", err)
		sess.Exit(1)
		return
	}
	defer ptmx.Close()

	// Set initial window size
	if err := pty.Setsize(ptmx, &pty.Winsize{
		Rows: uint16(ptyReq.Window.Height),
		Cols: uint16(ptyReq.Window.Width),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "failed to set PTY size: %v\n", err)
	}

	// Handle window resize
	go func() {
		for win := range winCh {
			pty.Setsize(ptmx, &pty.Winsize{
				Rows: uint16(win.Height),
				Cols: uint16(win.Width),
			})
		}
	}()

	// Bidirectional copy
	go func() {
		io.Copy(ptmx, sess)
	}()
	io.Copy(sess, ptmx)

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				sess.Exit(status.ExitStatus())
				return
			}
		}
		sess.Exit(1)
		return
	}
	sess.Exit(0)
}

// runWithoutPTY runs a command without a pseudo-terminal.
func (s *Server) runWithoutPTY(sess ssh.Session, cmd *exec.Cmd) {
	// Use pipes for proper stdin handling - this ensures stdin is closed
	// when the session's stdin is closed, preventing commands from hanging.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		sess.Exit(1)
		return
	}

	cmd.Stdout = sess
	cmd.Stderr = sess.Stderr()

	if err := cmd.Start(); err != nil {
		sess.Exit(1)
		return
	}

	// Copy stdin in a goroutine and close when done
	go func() {
		io.Copy(stdin, sess)
		stdin.Close()
	}()

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				sess.Exit(status.ExitStatus())
				return
			}
		}
		sess.Exit(1)
		return
	}
	sess.Exit(0)
}

// sftpHandler handles SFTP subsystem requests.
func (s *Server) sftpHandler(sess ssh.Session) {
	server, err := sftp.NewServer(sess)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create SFTP server: %v\n", err)
		return
	}
	if err := server.Serve(); err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "SFTP server error: %v\n", err)
	}
}

// setupHostKey loads or generates a host key.
func (s *Server) setupHostKey(server *ssh.Server) error {
	// Try to load existing host key
	if s.hostKeyPath != "" {
		if keyBytes, err := os.ReadFile(s.hostKeyPath); err == nil {
			if signer, err := gossh.ParsePrivateKey(keyBytes); err == nil {
				server.AddHostKey(signer)
				return nil
			}
		}
	}

	// Generate new host key using ed25519
	pubKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("failed to generate host key: %w", err)
	}

	// Create signer directly from the private key
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		return fmt.Errorf("failed to create signer: %w", err)
	}
	server.AddHostKey(signer)

	_ = pubKey // Silence unused warning

	return nil
}

// stdioConn wraps stdin/stdout as net.Conn for SSH server.
type stdioConn struct {
	io.Reader
	io.Writer
}

func (c *stdioConn) Close() error {
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
