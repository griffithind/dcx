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
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"time"

	"github.com/gliderlabs/ssh"
	"github.com/griffithind/dcx/internal/agentproto"
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

	sessionConfigPath string        // agentproto.SessionConfigPath (tests override)
	hangupGrace       time.Duration // see defaultHangupGrace
}

// NewServer builds a server from a Config.
func NewServer(cfg Config) (*Server, error) {
	if cfg.Shell == "" {
		return nil, fmt.Errorf("shell is required")
	}

	s := &Server{
		cfg:               cfg,
		shellConfig:       SetupShellIntegration(cfg.Shell),
		sessionConfigPath: agentproto.SessionConfigPath,
		hangupGrace:       defaultHangupGrace,
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

// authorize returns true if the presented public key appears in any of the
// configured AuthorizedKeysPaths. Reads are performed per-attempt because
// authorized_keys can be rewritten at any time via runtime secret re-mount.
//
// Only the configured, dcx-managed files are consulted. The SSH username the
// client presents is deliberately ignored: every session runs as cfg.User,
// so trusting "~<client user>/.ssh/authorized_keys" would let any container
// user who can write their own authorized_keys obtain a session as cfg.User.
func (s *Server) authorize(_ ssh.Context, presented ssh.PublicKey) bool {
	for _, path := range s.cfg.AuthorizedKeysPaths {
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
		// Conventional fallback when the user is not in /etc/passwd.
		home := "/home/" + s.cfg.User
		if s.cfg.User == "" || s.cfg.User == "root" {
			home = "/root"
		}
		env = append(env, "HOME="+home)
	}

	// Set shell
	env = append(env, "SHELL="+s.cfg.Shell)

	// Add shell integration env vars for terminal titles (from cached config)
	env = append(env, s.shellConfig.Env...)

	return env
}

// sftpHandler handles SFTP subsystem requests.
//
// When sessions drop privileges (agent runs as root, cfg.User is not root),
// SFTP must drop them too, otherwise every authorized key gets root
// filesystem access and IDE-created files end up owned by root. The agent
// re-executes itself as `dcx-agent sftp-server` under the target user's
// credentials and pipes the session through it. Without a privilege drop the
// server runs in-process.
func (s *Server) sftpHandler(sess ssh.Session) {
	if s.sessionCredential() == nil {
		serveSFTP(sess, s.resolveWorkDir())
		return
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "sftp: cannot locate agent binary: %v\n", err)
		_ = sess.Exit(1)
		return
	}
	workDir := s.resolveWorkDir()
	cmd := exec.Command(self, "sftp-server", "--workdir", workDir)
	cmd.Dir = workDir
	cmd.Env = s.buildEnvironment()
	s.applyUserCredentials(cmd)
	cmd.Stdin = sess
	cmd.Stdout = sess
	cmd.Stderr = sess.Stderr()

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "sftp-server: %v\n", err)
		_ = sess.Exit(1)
		return
	}
	_ = sess.Exit(0)
}

// serveSFTP runs an SFTP server over rwc until the client disconnects.
func serveSFTP(rwc io.ReadWriteCloser, workDir string) {
	var opts []sftp.ServerOption
	if workDir != "" {
		opts = append(opts, sftp.WithServerWorkingDirectory(workDir))
	}
	server, err := sftp.NewServer(rwc, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create SFTP server: %v\n", err)
		return
	}
	if err := server.Serve(); err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "SFTP server error: %v\n", err)
	}
}

// setupHostKey loads the persistent host key. dcx writes it to the
// container from ~/.dcx/hostkeys/ before launching the agent, and its
// client pins exactly that key, so a missing or corrupt file is a hard
// error: generating a key here would only produce a host key no client
// trusts.
func (s *Server) setupHostKey(server *ssh.Server) error {
	if s.cfg.HostKeyPath == "" {
		return fmt.Errorf("HostKeyPath is required")
	}
	keyBytes, err := os.ReadFile(s.cfg.HostKeyPath)
	if err != nil {
		return fmt.Errorf("read host key: %w", err)
	}
	signer, err := gossh.ParsePrivateKey(keyBytes)
	if err != nil {
		return fmt.Errorf("host key at %s is unparseable: %w", s.cfg.HostKeyPath, err)
	}
	server.AddHostKey(signer)
	return nil
}
