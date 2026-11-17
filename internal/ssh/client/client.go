// Package client runs lifecycle hook commands inside dcx-managed
// containers over the dcx-agent's SSH listener, from within dcx itself.
// Users and editors connect with OpenSSH instead (`dcx ssh`, `ssh <alias>`);
// this client exists because hooks run in-process, stream their output
// into dcx's and must be cancellable.
//
// Sessions get their environment (remoteEnv, userEnvProbe) from the agent,
// like every other session, so the client sends none.
package client

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/shell"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// RunOptions describes one non-interactive command.
type RunOptions struct {
	// Port is the host loopback port the container's agent listens on
	// (container.SSHHostPort).
	Port        int
	WorkspaceID string // locates the workspace's pinned host key
	Command     []string
	Stdout      io.Writer
	Stderr      io.Writer
	// TTY runs the command on a pseudo-terminal of the given size, as
	// devcontainers/cli runs lifecycle commands: colours, progress bars
	// and `docker run -it` work. Output then all arrives on Stdout.
	TTY *TerminalSize
}

// TerminalSize is a TTY's size and $TERM.
type TerminalSize struct {
	Term       string
	Rows, Cols int
}

// Run runs a command in the container and returns its exit code. Stdin is
// empty: a hook reading the user's terminal would leave a goroutine
// consuming input meant for whatever runs next.
//
// Cancelling ctx (Ctrl-C, a failed parallel hook) signals the remote
// command and closes the session; Run then returns ctx.Err().
func Run(ctx context.Context, opts RunOptions) (int, error) {
	if len(opts.Command) == 0 {
		return -1, fmt.Errorf("no command")
	}
	client, err := connect(ctx, opts.Port, opts.WorkspaceID, "dcx")
	if err != nil {
		return -1, err
	}
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("new ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()

	// Agent forwarding is optional (hooks cloning private repos use it).
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		_ = agent.RequestAgentForwarding(session)
	}

	if t := opts.TTY; t != nil {
		modes := ssh.TerminalModes{ssh.ECHO: 0}
		if err := session.RequestPty(t.Term, t.Rows, t.Cols, modes); err != nil {
			return -1, fmt.Errorf("request pty: %w", err)
		}
	}

	session.Stdin = strings.NewReader("")
	session.Stdout = opts.Stdout
	session.Stderr = opts.Stderr

	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Signal(ssh.SIGTERM)
			_ = session.Close()
			_ = client.Close()
		case <-stopWatch:
		}
	}()

	runErr := session.Run(shell.Join(opts.Command))
	if ctx.Err() != nil {
		return -1, ctx.Err()
	}
	if runErr != nil {
		if ee, ok := runErr.(*ssh.ExitError); ok {
			return ee.ExitStatus(), nil
		}
		return -1, runErr
	}
	return 0, nil
}

// connect dials the dcx-agent on 127.0.0.1:port and completes the SSH
// handshake.
//
// The host key is verified against the workspace's persistent host key via
// FixedHostKey — dcx controls both ends, so no known_hosts round-trip is
// needed for this path (plain `ssh` clients use ~/.dcx/known_hosts).
func connect(ctx context.Context, port int, workspaceID, user string) (*ssh.Client, error) {
	if port <= 0 {
		return nil, fmt.Errorf("no SSH port for the container")
	}

	hostKeyCallback, err := hostKeyCallbackForWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:            user,
		Auth:            clientAuthMethods(),
		HostKeyCallback: hostKeyCallback,
		Timeout:         10 * time.Second,
	}

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	dialer := &net.Dialer{Timeout: config.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)

	// Handle agent-forwarding requests from the server.
	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		go func() {
			agentChans := client.HandleChannelOpen("auth-agent@openssh.com")
			for newChannel := range agentChans {
				channel, reqs, err := newChannel.Accept()
				if err != nil {
					continue
				}
				go ssh.DiscardRequests(reqs)

				agentConn, err := net.Dial("unix", sock)
				if err != nil {
					_ = channel.Close()
					continue
				}

				go func(ch ssh.Channel, c net.Conn) {
					defer func() { _ = ch.Close() }()
					defer func() { _ = c.Close() }()
					go func() { _, _ = io.Copy(ch, c) }()
					_, _ = io.Copy(c, ch)
				}(channel, agentConn)
			}
		}()
	}

	return client, nil
}

// hostKeyCallbackForWorkspace returns a ssh.HostKeyCallback that pins the
// workspace's stored host key. The key is the same one the agent loads from
// its bind-mounted /run/secrets/dcx/ssh_host_ed25519_key.
//
// The policy matches the agent's: the key is loaded, never generated or
// skipped. Without a workspace ID or a stored key there is nothing to pin,
// so the connection is refused rather than trusted blindly.
func hostKeyCallbackForWorkspace(workspaceID string) (ssh.HostKeyCallback, error) {
	_, signer, err := dcxssh.LoadHostKey(workspaceID)
	if err != nil {
		return nil, fmt.Errorf("load workspace host key: %w", err)
	}
	return ssh.FixedHostKey(signer.PublicKey()), nil
}

// clientAuthMethods returns the SSH auth methods used to authenticate to
// the dcx-agent. The public-key method is a single callback that returns
// the union of:
//
//   - SSH-agent identities at $SSH_AUTH_SOCK (yubikey, 1Password, ssh-agent)
//   - Private keys on disk (~/.ssh/id_ed25519, …_ecdsa, …_rsa)
//   - The dcx fallback key at ~/.dcx/id_ed25519
//
// They are merged into a single [ssh.PublicKeysCallback] instead of being
// passed as separate [ssh.PublicKeys] methods because the SSH client
// library treats "publickey" as a single auth method name and will not
// retry it after the first attempt exhausts — so a separate agent method
// returning zero signers would prevent fallback from being tried at all.
//
// Empty password is deliberately omitted — the server does not accept
// passwords.
func clientAuthMethods() []ssh.AuthMethod {
	return []ssh.AuthMethod{
		ssh.PublicKeysCallback(collectClientSigners),
	}
}

// collectClientSigners produces the signer list for authentication. It is
// called lazily by the SSH library once per handshake.
func collectClientSigners() ([]ssh.Signer, error) {
	var out []ssh.Signer

	if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
		if agentConn, err := net.Dial("unix", sock); err == nil {
			if signers, err := agent.NewClient(agentConn).Signers(); err == nil {
				out = append(out, signers...)
			}
		}
	}

	if home, err := os.UserHomeDir(); err == nil {
		for _, path := range []string{
			home + "/.ssh/id_ed25519",
			home + "/.ssh/id_ecdsa",
			home + "/.ssh/id_rsa",
			home + "/.dcx/id_ed25519",
		} {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			signer, err := ssh.ParsePrivateKey(data)
			if err != nil {
				continue
			}
			out = append(out, signer)
		}
	}

	return out, nil
}
