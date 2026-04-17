// Package server implements the TCP SSH server that runs inside dcx-managed
// containers. It is bundled into the `dcx-agent` binary and invoked as
// `dcx-agent listen`.
//
// All client connections complete public-key authentication against a
// mounted authorized_keys file. A pre-handshake IP gate rejects any source
// address outside the configured allowlist (loopback by default).
package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/griffithind/dcx/internal/common"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
)

// Config holds everything required to build a Server.
type Config struct {
	User        string
	Shell       string
	WorkDir     string
	HostKeyPath string

	// Paths (in order) to check for authorized pubkeys. First match wins.
	// Typically [DCXSecretPath("authorized_keys"),
	// filepath.Join(userHome, ".ssh", "authorized_keys")].
	AuthorizedKeysPaths []string

	// Pre-handshake loopback gate. A non-loopback remote address has its
	// connection closed before any SSH bytes are exchanged.
	Gate *Gate

	// Path to write the ready sentinel once Accept is live. Empty means skip
	// the sentinel (useful for tests). In production this points at
	// /var/lib/dcx/agent-ready inside the container, readable via
	// `docker exec cat`.
	ReadyFile string
}

// Server is the SSH server.
type Server struct {
	server      *ssh.Server
	cfg         Config
	shellConfig ShellConfig // Cached shell integration config
}

// NewServer builds a server from a Config.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Shell == "" {
		return nil, fmt.Errorf("shell is required")
	}

	s := &Server{
		cfg:         cfg,
		shellConfig: SetupShellIntegration(cfg.Shell),
	}

	server := &ssh.Server{
		Handler: s.sessionHandler,
		SubsystemHandlers: map[string]ssh.SubsystemHandler{
			"sftp": s.sftpHandler,
		},
		// Allow outgoing tunnels to loopback destinations inside the container
		// (VS Code / JetBrains / Claude Desktop rely on direct-tcpip for their
		// in-container agent channels) but reject anything else.
		LocalPortForwardingCallback: func(ctx ssh.Context, dhost string, dport uint32) bool {
			return dhost == "127.0.0.1" || dhost == "::1" || dhost == "localhost"
		},
		// Reverse port forwarding is off by default — an authenticated client
		// should not be able to bind new listeners on the container interface.
		ReversePortForwardingCallback: func(ctx ssh.Context, host string, port uint32) bool {
			return false
		},
		ChannelHandlers: map[string]ssh.ChannelHandler{
			"direct-tcpip": ssh.DirectTCPIPHandler,
			"session":      ssh.DefaultSessionHandler,
		},
		// tcpip-forward / cancel-tcpip-forward handlers intentionally omitted
		// so reverse forwarding is not even advertised.
	}

	if cfg.Gate == nil {
		return nil, fmt.Errorf("Gate is required")
	}
	if len(cfg.AuthorizedKeysPaths) == 0 {
		return nil, fmt.Errorf("at least one AuthorizedKeysPath is required")
	}

	server.ConnCallback = func(ctx ssh.Context, conn net.Conn) net.Conn {
		if !cfg.Gate.Allow(conn.RemoteAddr()) {
			fmt.Fprintf(os.Stderr, "dcx-agent: rejected non-loopback connection from %s\n", conn.RemoteAddr())
			_ = conn.Close()
			return nil
		}
		return conn
	}
	if err := server.SetOption(ssh.PublicKeyAuth(s.authorize)); err != nil {
		return nil, fmt.Errorf("set public key auth: %w", err)
	}
	// Password auth is not configured — gliderlabs disables it when no
	// handler is set, which is what we want. There is no password path.

	// Load or generate host key.
	if err := s.setupHostKey(server); err != nil {
		return nil, err
	}

	s.server = server
	return s, nil
}

// Listen binds a TCP listener on addr and serves until ctx is cancelled.
//
// When ctx is cancelled, in-flight sessions are given 10 seconds to drain
// via ssh.Server.Shutdown; if any remain, they are forcibly closed.
func (s *Server) Listen(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	// Best-effort: write a sentinel file so host-side smoke tests and
	// idempotency probes can distinguish "starting" from "up".
	if s.cfg.ReadyFile != "" {
		writeReadyFile(s.cfg.ReadyFile, ln.Addr().String())
	}

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- s.server.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			// Shutdown timed out — force all connections closed.
			_ = s.server.Close()
		}
		<-serveErr
		return nil
	case err := <-serveErr:
		if errors.Is(err, ssh.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// writeReadyFile drops a minimal heartbeat file at path.
// Best-effort; errors are silent because readiness is advisory.
func writeReadyFile(path, listenAddr string) {
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	_ = os.WriteFile(path, []byte(listenAddr+"\n"), 0644)
}

// DefaultReadyFilePath is the sentinel the agent writes once Accept is live
// inside the container. Readable from the host via `docker exec cat`.
const DefaultReadyFilePath = "/var/lib/dcx/agent-ready"

// authorize returns true if the presented public key appears in any of the
// configured AuthorizedKeysPaths. Reads are performed per-attempt because
// authorized_keys can be rewritten at any time via runtime secret re-mount.
func (s *Server) authorize(ctx ssh.Context, presented ssh.PublicKey) bool {
	paths := make([]string, 0, len(s.cfg.AuthorizedKeysPaths)+1)
	paths = append(paths, s.cfg.AuthorizedKeysPaths...)

	// Also honor the session user's own ~/.ssh/authorized_keys.
	if u, err := user.Lookup(ctx.User()); err == nil {
		paths = append(paths, filepath.Join(u.HomeDir, ".ssh", "authorized_keys"))
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if matchAuthorizedKey(data, presented) {
			return true
		}
	}
	return false
}

// matchAuthorizedKey reports whether any public key in the authorized_keys
// content matches the presented key.
func matchAuthorizedKey(authorizedKeys []byte, presented ssh.PublicKey) bool {
	rest := bytes.TrimSpace(authorizedKeys)
	for len(rest) > 0 {
		pub, _, _, next, err := ssh.ParseAuthorizedKey(rest)
		if err != nil {
			return false
		}
		if ssh.KeysEqual(pub, presented) {
			return true
		}
		rest = next
	}
	return false
}

// sessionHandler handles SSH session requests.
func (s *Server) sessionHandler(sess ssh.Session) {
	// Setup agent forwarding if requested
	var agentSock string
	if ssh.AgentRequested(sess) {
		ln, err := ssh.NewAgentListener()
		if err == nil {
			defer func() { _ = ln.Close() }()
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
		cmd = exec.Command(s.cfg.Shell, "-c", rawCmd)
	} else {
		// Start an interactive shell with integration
		cmd = exec.Command(s.cfg.Shell)
		if isPty {
			// Use cached shell integration config
			if len(s.shellConfig.Args) > 0 {
				cmd.Args = append(cmd.Args, s.shellConfig.Args...)
			} else {
				// Default: login shell
				cmd.Args = append(cmd.Args, "-l")
			}
		}
	}

	cmd.Dir = s.resolveWorkDir()
	cmd.Env = s.buildEnvironment()
	s.applyUserCredentials(cmd)

	return cmd
}

// applyUserCredentials configures cmd to run as s.cfg.User when the agent
// process itself has a different effective UID (the common case: agent runs
// as root, session should drop privs to the devcontainer's remoteUser).
// Without this, every session would run with the agent's UID, defeating
// remoteUser.
//
// No-op when the target already matches the current euid, when user lookup
// fails, or when the uid/gid cannot be parsed — in all those cases the
// command inherits the agent's credentials.
func (s *Server) applyUserCredentials(cmd *exec.Cmd) {
	if s.cfg.User == "" {
		return
	}
	u, err := user.Lookup(s.cfg.User)
	if err != nil {
		return
	}
	uid, err := parseUint32(u.Uid)
	if err != nil {
		return
	}
	gid, err := parseUint32(u.Gid)
	if err != nil {
		return
	}
	if uid == uint32(os.Geteuid()) {
		return
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uid, Gid: gid},
	}
}

// parseUint32 parses a decimal uid/gid string into a uint32.
func parseUint32(s string) (uint32, error) {
	var v uint32
	if s == "" {
		return 0, fmt.Errorf("empty uid/gid")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid uid/gid %q", s)
		}
		v = v*10 + uint32(c-'0')
	}
	return v, nil
}

// resolveWorkDir returns the configured working directory if it exists,
// falling back to the user's home directory, then `/`. Setting cmd.Dir to a
// path that doesn't exist causes exec.Start to fail with ENOENT — users see
// the same as an auth error from the client side.
func (s *Server) resolveWorkDir() string {
	if s.cfg.WorkDir != "" {
		if stat, err := os.Stat(s.cfg.WorkDir); err == nil && stat.IsDir() {
			return s.cfg.WorkDir
		}
	}
	if u, err := user.Lookup(s.cfg.User); err == nil && u.HomeDir != "" {
		if stat, err := os.Stat(u.HomeDir); err == nil && stat.IsDir() {
			return u.HomeDir
		}
	}
	return "/"
}

// buildEnvironment creates the environment for the shell.
func (s *Server) buildEnvironment() []string {
	env := os.Environ()

	// Set user-related environment variables
	env = append(env, "USER="+s.cfg.User)
	env = append(env, "LOGNAME="+s.cfg.User)

	// Try to get home directory
	if u, err := user.Lookup(s.cfg.User); err == nil {
		env = append(env, "HOME="+u.HomeDir)
	} else {
		env = append(env, "HOME="+common.GetDefaultHomeDir(s.cfg.User))
	}

	// Set shell
	env = append(env, "SHELL="+s.cfg.Shell)

	// Add shell integration env vars for terminal titles (from cached config)
	env = append(env, s.shellConfig.Env...)

	return env
}

// runWithPTY runs a command with a pseudo-terminal.
func (s *Server) runWithPTY(sess ssh.Session, cmd *exec.Cmd, ptyReq ssh.Pty, winCh <-chan ssh.Window) {
	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start PTY: %v\n", err)
		_ = sess.Exit(1)
		return
	}
	defer func() { _ = ptmx.Close() }()

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
			_ = pty.Setsize(ptmx, &pty.Winsize{
				Rows: uint16(win.Height),
				Cols: uint16(win.Width),
			})
		}
	}()

	// Bidirectional copy
	go func() {
		_, _ = io.Copy(ptmx, sess)
	}()
	_, _ = io.Copy(sess, ptmx)

	// Wait for command to finish
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				_ = sess.Exit(status.ExitStatus())
				return
			}
		}
		_ = sess.Exit(1)
		return
	}
	_ = sess.Exit(0)
}

// runWithoutPTY runs a command without a pseudo-terminal.
func (s *Server) runWithoutPTY(sess ssh.Session, cmd *exec.Cmd) {
	// Use pipes for proper stdin handling - this ensures stdin is closed
	// when the session's stdin is closed, preventing commands from hanging.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = sess.Exit(1)
		return
	}

	cmd.Stdout = sess
	cmd.Stderr = sess.Stderr()

	if err := cmd.Start(); err != nil {
		_ = sess.Exit(1)
		return
	}

	// Copy stdin in a goroutine and close when done
	go func() {
		_, _ = io.Copy(stdin, sess)
		_ = stdin.Close()
	}()

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
				_ = sess.Exit(status.ExitStatus())
				return
			}
		}
		_ = sess.Exit(1)
		return
	}
	_ = sess.Exit(0)
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

// setupHostKey loads the persistent host key, or generates and persists a
// fresh one if the file is missing. A corrupt/unparseable file is treated
// as a hard error — silently regenerating would invalidate every pinned
// client's known_hosts entry.
//
// The path normally points at a bind-mounted
// /run/secrets/dcx/ssh_host_ed25519_key sourced from the host's
// ~/.dcx/hostkeys/ directory.
func (s *Server) setupHostKey(server *ssh.Server) error {
	if s.cfg.HostKeyPath == "" {
		return fmt.Errorf("HostKeyPath is required")
	}

	if keyBytes, err := os.ReadFile(s.cfg.HostKeyPath); err == nil {
		signer, perr := gossh.ParsePrivateKey(keyBytes)
		if perr != nil {
			return fmt.Errorf("host key at %s is unparseable: %w", s.cfg.HostKeyPath, perr)
		}
		server.AddHostKey(signer)
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read host key at %s: %w", s.cfg.HostKeyPath, err)
	}

	// Missing file — generate and persist.
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("generate host key: %w", err)
	}
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		return fmt.Errorf("create signer: %w", err)
	}
	server.AddHostKey(signer)

	pemBlock, err := gossh.MarshalPrivateKey(privateKey, "dcx-agent host key")
	if err != nil {
		return fmt.Errorf("marshal host key: %w", err)
	}
	if err := os.WriteFile(s.cfg.HostKeyPath, pem.EncodeToMemory(pemBlock), 0600); err != nil {
		return fmt.Errorf("persist host key: %w", err)
	}

	return nil
}
