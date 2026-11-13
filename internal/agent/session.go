package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gliderlabs/ssh"
	"github.com/griffithind/dcx/internal/agentproto"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

// defaultHangupGrace is how long a session's processes get to exit after
// the client disconnects before they are killed.
const defaultHangupGrace = 5 * time.Second

// sessionHandler handles SSH session requests. Every session, whatever the
// client (dcx, OpenSSH, an editor), gets the same user, environment and
// postAttachCommand.
func (s *Server) sessionHandler(sess ssh.Session) {
	cred, err := s.sessionCredential()
	if err != nil {
		_, _ = fmt.Fprintf(sess.Stderr(), "dcx-agent: %v\n", err)
		_ = sess.Exit(1)
		return
	}
	cfg := loadSessionConfig(s.sessionConfigPath)

	var agentSock string
	if ssh.AgentRequested(sess) {
		ln, dir, err := agentListener(cred)
		if err != nil {
			fmt.Fprintf(os.Stderr, "dcx-agent: agent forwarding unavailable: %v\n", err)
		} else {
			defer func() {
				_ = ln.Close()
				_ = os.RemoveAll(dir)
			}()
			go ssh.ForwardAgentConnections(ln, sess)
			agentSock = ln.Addr().String()
		}
	}

	ptyReq, winCh, isPty := sess.Pty()
	env := s.sessionEnvironment(cfg, sess.Environ())
	if agentSock != "" {
		env = append(env, "SSH_AUTH_SOCK="+agentSock)
	}
	if isPty {
		env = append(env, "TERM="+ptyReq.Term)
	}

	// postAttachCommand belongs to interactive attaches: a terminal without
	// a command (`dcx ssh`, `ssh <alias>`), not editor exec channels.
	if isPty && sess.RawCommand() == "" && len(cfg.PostAttach) > 0 {
		s.runPostAttach(sess, cfg.PostAttach, env, cred)
	}

	cmd := s.buildCommand(sess, isPty)
	cmd.Env = env
	applyCredential(cmd, cred)

	if isPty {
		s.runWithPTY(sess, cmd, ptyReq, winCh)
	} else {
		s.runWithoutPTY(sess, cmd)
	}
}

// buildCommand creates an exec.Cmd for the session.
func (s *Server) buildCommand(sess ssh.Session, isPty bool) *exec.Cmd {
	var cmd *exec.Cmd

	// RawCommand is the exact command string the client sent, run via the
	// shell's -c like OpenSSH does.
	if rawCmd := sess.RawCommand(); rawCmd != "" {
		cmd = exec.Command(s.cfg.Shell, "-c", rawCmd)
	} else {
		cmd = exec.Command(s.cfg.Shell)
		if isPty {
			if len(s.shellConfig.Args) > 0 {
				cmd.Args = append(cmd.Args, s.shellConfig.Args...)
			} else {
				cmd.Args = append(cmd.Args, "-l")
			}
		}
	}
	cmd.Dir = s.resolveWorkDir()
	return cmd
}

// sessionEnvironment is the session's environment: the agent's base
// environment, then the devcontainer's session env (userEnvProbe result
// and remoteEnv), then what the client sent. exec uses the last value of a
// duplicated key.
func (s *Server) sessionEnvironment(cfg agentproto.SessionConfig, clientEnv []string) []string {
	env := s.buildEnvironment()
	keys := make([]string, 0, len(cfg.Env))
	for k := range cfg.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+cfg.Env[k])
	}
	if len(cfg.Unset) > 0 {
		env = slices.DeleteFunc(env, func(kv string) bool {
			name, _, _ := strings.Cut(kv, "=")
			return slices.Contains(cfg.Unset, name)
		})
	}
	return append(env, clientEnv...)
}

// loadSessionConfig reads the session configuration dcx wrote. A missing or
// unreadable file means no extra environment and no postAttachCommand.
func loadSessionConfig(path string) agentproto.SessionConfig {
	var cfg agentproto.SessionConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "dcx-agent: ignoring unreadable %s: %v\n", path, err)
		return agentproto.SessionConfig{}
	}
	return cfg
}

// sessionCredential returns the credential sessions must run with, or nil
// when no privilege change is needed. It carries the user's supplementary
// groups: without them a session would silently lose membership of groups
// such as docker or sudo.
//
// The user is looked up by name, then by numeric ID; a numeric ID with no
// passwd entry runs as that UID (and GID), as `docker run --user 1000`
// does. A configured user that cannot be resolved is an error: falling back
// to the agent's own identity would run the session as root.
func (s *Server) sessionCredential() (*syscall.Credential, error) {
	if s.cfg.User == "" {
		return nil, nil
	}
	u, err := lookupUser(s.cfg.User)
	if err != nil {
		if id, perr := parseUint32(s.cfg.User); perr == nil {
			if id == uint32(os.Geteuid()) {
				return nil, nil
			}
			return &syscall.Credential{Uid: id, Gid: id}, nil
		}
		return nil, fmt.Errorf("session user %q not found in the container", s.cfg.User)
	}
	uid, err := parseUint32(u.Uid)
	if err != nil {
		return nil, fmt.Errorf("session user %q: %w", s.cfg.User, err)
	}
	gid, err := parseUint32(u.Gid)
	if err != nil {
		return nil, fmt.Errorf("session user %q: %w", s.cfg.User, err)
	}
	if uid == uint32(os.Geteuid()) {
		return nil, nil
	}
	cred := &syscall.Credential{Uid: uid, Gid: gid}
	if ids, err := u.GroupIds(); err == nil {
		for _, id := range ids {
			if g, err := parseUint32(id); err == nil {
				cred.Groups = append(cred.Groups, g)
			}
		}
	}
	return cred, nil
}

// lookupUser finds a user by name, then by numeric ID.
func lookupUser(name string) (*user.User, error) {
	if u, err := user.Lookup(name); err == nil {
		return u, nil
	}
	return user.LookupId(name)
}

func applyCredential(cmd *exec.Cmd, cred *syscall.Credential) {
	if cred == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = cred
}

// agentListener creates the forwarded SSH agent socket in a fresh private
// directory. When sessions drop privileges, the directory and socket are
// handed to the session user; otherwise only the agent's own user could
// reach them and forwarding would silently fail. The caller removes dir.
func agentListener(cred *syscall.Credential) (net.Listener, string, error) {
	dir, err := os.MkdirTemp("", "dcx-agent-fwd-")
	if err != nil {
		return nil, "", err
	}
	sock := filepath.Join(dir, "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, "", err
	}
	if cred != nil {
		for _, p := range []string{dir, sock} {
			if err := os.Chown(p, int(cred.Uid), int(cred.Gid)); err != nil {
				_ = ln.Close()
				_ = os.RemoveAll(dir)
				return nil, "", err
			}
		}
	}
	return ln, dir, nil
}

// runPostAttach runs the postAttachCommand steps before an interactive
// shell starts. Output goes to the terminal; a failure is reported there
// and does not prevent the shell from starting.
func (s *Server) runPostAttach(sess ssh.Session, steps []agentproto.AttachStep, env []string, cred *syscall.Credential) {
	out := &lockedWriter{w: &crlfWriter{w: sess}}
	for _, step := range steps {
		if err := s.runAttachStep(sess, step, env, cred, out); err != nil {
			_, _ = fmt.Fprintf(out, "dcx: postAttachCommand (%s) failed: %v\n", step.Label, err)
			return
		}
	}
}

func (s *Server) runAttachStep(sess ssh.Session, step agentproto.AttachStep, env []string, cred *syscall.Credential, out io.Writer) error {
	errs := make([]error, len(step.Commands))
	var wg sync.WaitGroup
	for i, c := range step.Commands {
		wg.Add(1)
		go func(i int, c agentproto.AttachCommand) {
			defer wg.Done()
			var cmd *exec.Cmd
			switch {
			case c.Shell != "":
				cmd = exec.CommandContext(sess.Context(), "/bin/sh", "-c", c.Shell)
			case len(c.Argv) > 0:
				cmd = exec.CommandContext(sess.Context(), c.Argv[0], c.Argv[1:]...)
			default:
				return
			}
			cmd.Dir = s.resolveWorkDir()
			cmd.Env = env
			cmd.Stdout = out
			cmd.Stderr = out
			applyCredential(cmd, cred)
			if err := cmd.Run(); err != nil {
				if c.Name != "" {
					err = fmt.Errorf("[%s] %w", c.Name, err)
				}
				errs[i] = err
			}
		}(i, c)
	}
	wg.Wait()
	return errors.Join(errs...)
}

// runWithPTY runs a command with a pseudo-terminal.
func (s *Server) runWithPTY(sess ssh.Session, cmd *exec.Cmd, ptyReq ssh.Pty, winCh <-chan ssh.Window) {
	ptmx, err := startPTY(cmd, &pty.Winsize{
		Rows: uint16(ptyReq.Window.Height),
		Cols: uint16(ptyReq.Window.Width),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to start PTY: %v\n", err)
		_ = sess.Exit(1)
		return
	}
	// Resizes may still arrive while the session ends; never resize a
	// closed terminal.
	var ptmxMu sync.Mutex
	ptmxClosed := false
	closePTY := func() {
		ptmxMu.Lock()
		defer ptmxMu.Unlock()
		if !ptmxClosed {
			ptmxClosed = true
			_ = ptmx.Close()
		}
	}
	defer closePTY()
	stop := superviseProcess(sess, cmd.Process.Pid, s.hangupGrace)
	defer stop()
	go func() {
		for win := range winCh {
			ptmxMu.Lock()
			if !ptmxClosed {
				_ = setWinsize(ptmx, uint16(win.Height), uint16(win.Width))
			}
			ptmxMu.Unlock()
		}
	}()

	go func() {
		_, _ = io.Copy(ptmx, sess)
	}()
	copied := make(chan struct{})
	go func() {
		_, _ = io.Copy(sess, ptmx)
		close(copied)
	}()

	// The session ends with its command. Reading the terminal only stops
	// once every process holding it has closed it, so a background job
	// left running would keep the session open forever: after the command
	// exits, drain what it wrote for a moment, then close the terminal
	// (which hangs up the job, as closing an ssh session does).
	waitErr := cmd.Wait()
	select {
	case <-copied:
	case <-time.After(ptyDrainTimeout):
		closePTY()
		<-copied
	}
	exitSession(sess, waitErr)
}

// pollable returns f re-opened in non-blocking mode, so Go's poller serves
// its reads and Close interrupts a pending one. pty.Open leaves the master
// in blocking mode (its ioctls go through File.Fd), where a read blocked on
// a terminal another process still holds can never be cancelled.
func pollable(f *os.File) (*os.File, error) {
	fd, err := unix.FcntlInt(f.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("dup pty: %w", err)
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("pty non-blocking: %w", err)
	}
	_ = f.Close()
	return os.NewFile(uintptr(fd), f.Name()), nil
}

// setWinsize resizes the terminal without File.Fd, which would put the
// master back into blocking mode.
func setWinsize(f *os.File, rows, cols uint16) error {
	sc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	if err := sc.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{Row: rows, Col: cols})
	}); err != nil {
		return err
	}
	return ioctlErr
}

// ptyDrainTimeout bounds how long a PTY session keeps copying output after
// its command exited while something else still holds the terminal.
const ptyDrainTimeout = 250 * time.Millisecond

// startPTY starts cmd on a new pseudo-terminal as a session leader
// (Setsid), so the whole job can be signalled through its process group.
// When the command drops privileges, the terminal device is handed to the
// session user: tools that reopen /dev/tty (sudo, ssh, gpg pinentry) fail
// on a root-owned terminal.
func startPTY(cmd *exec.Cmd, size *pty.Winsize) (*os.File, error) {
	opened, tty, err := pty.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tty.Close() }()
	ptmx, err := pollable(opened)
	if err != nil {
		_ = opened.Close()
		return nil, err
	}

	if err := setWinsize(ptmx, size.Rows, size.Cols); err != nil {
		fmt.Fprintf(os.Stderr, "failed to set PTY size: %v\n", err)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if cred := cmd.SysProcAttr.Credential; cred != nil {
		if err := tty.Chown(int(cred.Uid), -1); err != nil {
			_ = ptmx.Close()
			return nil, fmt.Errorf("chown pty: %w", err)
		}
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		return nil, err
	}
	return ptmx, nil
}

// runWithoutPTY runs a command without a pseudo-terminal.
func (s *Server) runWithoutPTY(sess ssh.Session, cmd *exec.Cmd) {
	// Own process group, so signals and hangup reach the whole job.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true

	// A pipe (not the session) as stdin, so the command sees EOF when the
	// client closes its stdin instead of hanging.
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = sess.Exit(1)
		return
	}
	cmd.Stdout = sess
	cmd.Stderr = sess.Stderr()

	if err := cmd.Start(); err != nil {
		_, _ = fmt.Fprintf(sess.Stderr(), "dcx-agent: %v\n", err)
		_ = sess.Exit(127)
		return
	}
	stop := superviseProcess(sess, cmd.Process.Pid, s.hangupGrace)
	defer stop()

	go func() {
		_, _ = io.Copy(stdin, sess)
		_ = stdin.Close()
	}()

	exitSession(sess, cmd.Wait())
}

// superviseProcess forwards the client's signal requests to the process
// group led by pid and hangs the job up when the client disconnects (then
// kills it if it ignores SIGHUP). Without this, a command whose client went
// away kept running forever. The returned func stops supervision.
func superviseProcess(sess ssh.Session, pid int, grace time.Duration) func() {
	sigs := make(chan ssh.Signal, 4)
	sess.Signals(sigs)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-sigs:
				if n, ok := signalNumbers[sig]; ok {
					_ = syscall.Kill(-pid, n)
				}
			case <-sess.Context().Done():
				_ = syscall.Kill(-pid, syscall.SIGHUP)
				select {
				case <-done:
				case <-time.After(grace):
					_ = syscall.Kill(-pid, syscall.SIGKILL)
				}
				return
			case <-done:
				return
			}
		}
	}()
	return func() {
		sess.Signals(nil)
		close(done)
	}
}

// exitSession reports how the command ended: exit-status for a normal
// exit, exit-signal (RFC 4254 §6.10) when it was killed by a signal, so
// clients can report e.g. 130 for Ctrl-C.
func exitSession(sess ssh.Session, err error) {
	if err == nil {
		_ = sess.Exit(0)
		return
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if ws.Signaled() {
				if name, ok := signalNames[ws.Signal()]; ok {
					payload := gossh.Marshal(struct {
						Signal     string
						CoreDumped bool
						Error      string
						Lang       string
					}{name, ws.CoreDump(), "", ""})
					_, _ = sess.SendRequest("exit-signal", false, payload)
					_ = sess.Close()
					return
				}
				_ = sess.Exit(128 + int(ws.Signal()))
				return
			}
			_ = sess.Exit(ws.ExitStatus())
			return
		}
	}
	_ = sess.Exit(1)
}

var signalNumbers = map[ssh.Signal]syscall.Signal{
	ssh.SIGABRT: syscall.SIGABRT,
	ssh.SIGALRM: syscall.SIGALRM,
	ssh.SIGFPE:  syscall.SIGFPE,
	ssh.SIGHUP:  syscall.SIGHUP,
	ssh.SIGILL:  syscall.SIGILL,
	ssh.SIGINT:  syscall.SIGINT,
	ssh.SIGKILL: syscall.SIGKILL,
	ssh.SIGPIPE: syscall.SIGPIPE,
	ssh.SIGQUIT: syscall.SIGQUIT,
	ssh.SIGSEGV: syscall.SIGSEGV,
	ssh.SIGTERM: syscall.SIGTERM,
	ssh.SIGUSR1: syscall.SIGUSR1,
	ssh.SIGUSR2: syscall.SIGUSR2,
}

var signalNames = func() map[syscall.Signal]string {
	m := make(map[syscall.Signal]string, len(signalNumbers))
	for name, n := range signalNumbers {
		m[n] = string(name)
	}
	return m
}()

// crlfWriter translates \n to \r\n, for output written to a terminal the
// client has put in raw mode.
type crlfWriter struct{ w io.Writer }

func (c *crlfWriter) Write(p []byte) (int, error) {
	if _, err := c.w.Write([]byte(strings.ReplaceAll(string(p), "\n", "\r\n"))); err != nil {
		return 0, err
	}
	return len(p), nil
}

// lockedWriter serializes writes from parallel commands.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}
