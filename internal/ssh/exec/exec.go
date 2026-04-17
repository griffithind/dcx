// Package exec is the host-side execution path for running commands inside
// dcx-managed containers. It dials the in-container dcx-agent's TCP SSH
// listener, authenticates with the user's SSH credentials, and runs a
// command or an interactive shell.
//
// Before the TCP transport, dcx used an SSH-over-stdio tunnel through
// `docker exec`. That package (internal/ssh/client) was renamed here when
// stdio was removed.
package exec

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/term"
)

// defaultContainerSSHPort is the port the dcx-agent listens on inside the
// container. Docker -p maps it to an ephemeral host port which we discover
// at connect time via `docker port`.
const defaultContainerSSHPort = 48022

// ContainerExecOptions is the high-level options struct used by CLI
// commands (exec/shell/run) and lifecycle hooks.
type ContainerExecOptions struct {
	ContainerName string
	WorkspaceID   string // used to locate the per-workspace host key
	Config        *devcontainer.DevContainerConfig
	WorkspacePath string
	Command       []string  // nil = interactive shell
	Env           []string  // additional env vars (appended to defaults)
	Stdin         io.Reader // defaults to os.Stdin
	Stdout        io.Writer // defaults to os.Stdout
	Stderr        io.Writer // defaults to os.Stderr
	TTY           *bool     // nil = auto-detect from stdin
}

// ExecInContainer runs a command (or interactive shell) inside a container
// via SSH. Returns the exit code.
func ExecInContainer(ctx context.Context, opts ContainerExecOptions) (int, error) {
	user, workDir := resolveUserAndWorkDir(opts.Config, opts.WorkspacePath)

	env := buildExecEnvironment(user, opts.Config)
	env = append(env, opts.Env...)

	tty := false
	if opts.TTY != nil {
		tty = *opts.TTY
	} else if f, ok := opts.Stdin.(*os.File); ok {
		tty = term.IsTerminal(int(f.Fd()))
	} else if opts.Stdin == nil {
		tty = term.IsTerminal(int(os.Stdin.Fd()))
	}

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

	client, err := connect(ctx, opts.ContainerName, opts.WorkspaceID, user)
	if err != nil {
		return -1, err
	}
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return -1, fmt.Errorf("new ssh session: %w", err)
	}
	defer func() { _ = session.Close() }()

	// Agent forwarding is optional — best-effort.
	if os.Getenv("SSH_AUTH_SOCK") != "" {
		_ = agent.RequestAgentForwarding(session)
	}

	if tty {
		fd := int(os.Stdin.Fd())
		oldState, err := term.MakeRaw(fd)
		if err == nil {
			defer func() { _ = term.Restore(fd, oldState) }()
		}

		w, h, _ := term.GetSize(fd)
		if w == 0 {
			w = 80
		}
		if h == 0 {
			h = 24
		}

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
			return -1, fmt.Errorf("request pty: %w", err)
		}

		go handleWindowResize(ctx, session, fd)
	}

	for _, envKV := range env {
		if idx := strings.Index(envKV, "="); idx > 0 {
			// Setenv is optional per RFC 4254 §6.4; ignore errors.
			_ = session.Setenv(envKV[:idx], envKV[idx+1:])
		}
	}

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr

	// Set working directory (via env; the server respects it via Config.WorkDir
	// but for ssh-session cd we rely on the shell picking up PWD).
	_ = workDir // server uses its own workdir; preserved for future use

	var runErr error
	if len(opts.Command) == 0 {
		runErr = session.Shell()
		if runErr == nil {
			runErr = session.Wait()
		}
	} else {
		runErr = session.Run(joinCommandForShell(opts.Command))
	}

	if runErr != nil {
		if ee, ok := runErr.(*ssh.ExitError); ok {
			return ee.ExitStatus(), nil
		}
		if runErr == io.EOF {
			return 0, nil
		}
		return -1, runErr
	}
	return 0, nil
}

// connect dials the dcx-agent's TCP listener for containerName and
// completes the SSH handshake.
//
// The host key is verified against the workspace's persistent host key via
// FixedHostKey — dcx controls both ends, so no known_hosts round-trip is
// needed for this path (plain `ssh` clients use ~/.dcx/known_hosts).
func connect(ctx context.Context, containerName, workspaceID, user string) (*ssh.Client, error) {
	port, err := resolveSSHPort(ctx, containerName)
	if err != nil {
		return nil, fmt.Errorf("resolve ssh port for %s: %w", containerName, err)
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
func hostKeyCallbackForWorkspace(workspaceID string) (ssh.HostKeyCallback, error) {
	if workspaceID == "" {
		// No workspace ID known (e.g. stray lifecycle hook) — trust on first
		// use. Safe because we're dialing 127.0.0.1 and the agent already
		// authenticates our pubkey.
		return ssh.InsecureIgnoreHostKey(), nil
	}
	_, signer, err := dcxssh.EnsureHostKey(workspaceID)
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

// resolveSSHPort returns the ephemeral host port Docker mapped to the
// container's :48022. Callers are expected to have already stamped the port
// into a label at create time once the PR4 changes land; until then we
// shell out to `docker port`.
func resolveSSHPort(ctx context.Context, containerName string) (int, error) {
	d := container.MustDocker()
	details, err := d.InspectContainer(ctx, containerName)
	if err == nil && details != nil {
		if s := details.Labels["com.griffithind.dcx.ssh.host.port"]; s != "" {
			if n := atoi(s); n > 0 {
				return n, nil
			}
		}
	}
	return d.PortMapping(ctx, containerName, defaultContainerSSHPort, "tcp")
}

// atoi is a tiny positive-int parser that returns 0 on failure — matches
// util.StringToInt but kept local to avoid cross-package churn during the
// rename.
func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// handleWindowResize monitors terminal size changes and updates the SSH
// session.
func handleWindowResize(ctx context.Context, session *ssh.Session, fd int) {
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

// resolveUserAndWorkDir determines the user and working directory for
// container execution. It uses values from the devcontainer config if
// available, with sensible defaults.
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

	if user == "" {
		user = "root"
	}
	if workDir == "" {
		workDir = "/workspace"
	}

	return user, workDir
}

// buildExecEnvironment creates the base environment for container
// execution.
func buildExecEnvironment(user string, cfg *devcontainer.DevContainerConfig) []string {
	env := []string{
		"USER=" + user,
	}
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
func quoteForShell(s string) string {
	if s == "" {
		return "''"
	}

	if !shellUnsafeChars.MatchString(s) {
		return s
	}

	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}
