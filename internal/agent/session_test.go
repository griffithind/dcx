package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
	gossh "golang.org/x/crypto/ssh"
)

// startTestServer runs a listener on a loopback port and returns a
// connected client. sessionCfg, when non-nil, is written as the session
// configuration the agent reads.
func startTestServer(t *testing.T, sessionCfg *agentproto.SessionConfig) (*gossh.Client, string) {
	t.Helper()
	dir := t.TempDir()
	authKeysPath, signer := writeTestAuthorizedKeys(t, dir)

	cfgPath := filepath.Join(dir, "session.json")
	if sessionCfg != nil {
		data, err := json.Marshal(sessionCfg)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(cfgPath, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	readyFile := filepath.Join(dir, "ready")
	srv, err := NewServer(Config{
		Shell:               "/bin/sh",
		WorkDir:             dir,
		HostKeyPath:         writeTestHostKey(t, dir),
		AuthorizedKeysPaths: []string{authKeysPath},
		Gate:                Loopback(),
		ReadyFile:           readyFile,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv.sessionConfigPath, srv.hangupGrace = cfgPath, 500*time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Listen(ctx, "127.0.0.1:0") }()
	addr := waitForReadyAddr(t, readyFile, 2*time.Second)

	client, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            "test",
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Close()
		cancel()
		<-done
	})
	return client, dir
}

func run(t *testing.T, client *gossh.Client, command string) (string, error) {
	t.Helper()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	out, err := sess.CombinedOutput(command)
	return string(out), err
}

func TestSessionExitStatus(t *testing.T) {
	client, _ := startTestServer(t, nil)
	_, err := run(t, client, "exit 7")
	var exitErr *gossh.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitStatus() != 7 {
		t.Fatalf("want exit status 7, got %v", err)
	}
}

func TestSessionReportsExitSignal(t *testing.T) {
	client, _ := startTestServer(t, nil)
	_, err := run(t, client, "kill -TERM $$")
	var exitErr *gossh.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("want ExitError, got %v", err)
	}
	if exitErr.Signal() != "TERM" {
		t.Errorf("want exit-signal TERM, got signal %q status %d", exitErr.Signal(), exitErr.ExitStatus())
	}
}

func TestSessionForwardsSignals(t *testing.T) {
	client, _ := startTestServer(t, nil)
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	out := &syncBuffer{}
	sess.Stdout = out
	if err := sess.Start(`trap 'echo got-int; exit 3' INT; echo ready; while :; do sleep 0.05; done`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return strings.Contains(out.String(), "ready") })
	if err := sess.Signal(gossh.SIGINT); err != nil {
		t.Fatal(err)
	}
	err = sess.Wait()
	var exitErr *gossh.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitStatus() != 3 {
		t.Fatalf("want exit 3 from the INT trap, got %v (output %q)", err, out.String())
	}
	if !strings.Contains(out.String(), "got-int") {
		t.Errorf("trap did not run: %q", out.String())
	}
}

// A command whose client disconnects must not keep running.
func TestSessionHangsUpOnDisconnect(t *testing.T) {
	client, dir := startTestServer(t, nil)
	pidFile := filepath.Join(dir, "pid")
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := sess.Start("echo $$ > " + pidFile + "; exec sleep 60"); err != nil {
		t.Fatal(err)
	}
	var pid int
	waitFor(t, func() bool {
		data, err := os.ReadFile(pidFile)
		if err != nil {
			return false
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
		return err == nil && pid > 0
	})

	_ = client.Close()
	waitFor(t, func() bool { return syscall.Kill(pid, 0) != nil })
}

func TestSessionAppliesSessionEnv(t *testing.T) {
	client, _ := startTestServer(t, &agentproto.SessionConfig{
		Env: map[string]string{"DCX_TEST_REMOTE": "from-remoteEnv", "HOME": "/custom-home"},
	})
	out, err := run(t, client, `printf '%s|%s' "$DCX_TEST_REMOTE" "$HOME"`)
	if err != nil {
		t.Fatal(err, out)
	}
	if out != "from-remoteEnv|/custom-home" {
		t.Errorf("session env not applied: %q", out)
	}
}

func TestPostAttachRunsForInteractiveSessionsOnly(t *testing.T) {
	client, dir := startTestServer(t, nil)
	marker := filepath.Join(dir, "attached")
	cfg := agentproto.SessionConfig{PostAttach: []agentproto.AttachStep{
		{Label: "feature x", Commands: []agentproto.AttachCommand{{Shell: "echo feature >> " + marker}}},
		{Label: "devcontainer.json", Commands: []agentproto.AttachCommand{
			{Name: "a", Argv: []string{"sh", "-c", "echo a >> " + marker}},
			{Name: "b", Shell: "echo b >> " + marker},
		}},
	}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Exec sessions (editors, scp, dcx exec) do not attach.
	if out, err := run(t, client, "true"); err != nil {
		t.Fatal(err, out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("postAttachCommand ran for an exec session")
	}

	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.RequestPty("xterm", 24, 80, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	sess.Stdin = strings.NewReader("exit\n")
	var out bytes.Buffer
	sess.Stdout = &out
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	_ = sess.Wait()

	data, err = os.ReadFile(marker)
	if err != nil {
		t.Fatalf("postAttachCommand did not run: %v (output %q)", err, out.String())
	}
	lines := strings.Fields(string(data))
	if len(lines) != 3 || lines[0] != "feature" {
		t.Errorf("want the feature step first, then a and b; got %q", lines)
	}
}

func TestPostAttachFailureStopsStepsButNotShell(t *testing.T) {
	client, dir := startTestServer(t, nil)
	marker := filepath.Join(dir, "second")
	cfg := agentproto.SessionConfig{PostAttach: []agentproto.AttachStep{
		{Label: "first", Commands: []agentproto.AttachCommand{{Shell: "exit 4"}}},
		{Label: "second", Commands: []agentproto.AttachCommand{{Shell: "touch " + marker}}},
	}}
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(dir, "session.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.RequestPty("xterm", 24, 80, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	sess.Stdin = strings.NewReader("echo shell-ran\nexit\n")
	var out bytes.Buffer
	sess.Stdout = &out
	if err := sess.Shell(); err != nil {
		t.Fatal(err)
	}
	_ = sess.Wait()

	if !strings.Contains(out.String(), "postAttachCommand (first) failed") {
		t.Errorf("failure not reported: %q", out.String())
	}
	if !strings.Contains(out.String(), "shell-ran") {
		t.Errorf("shell did not start after the failure: %q", out.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Error("steps after a failed step must not run")
	}
}

func TestAgentListenerCreatesPrivateSocket(t *testing.T) {
	ln, dir, err := agentListener(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	defer func() { _ = ln.Close() }()
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Errorf("socket dir mode = %v, want 0700", st.Mode().Perm())
	}
	if filepath.Dir(ln.Addr().String()) != dir {
		t.Errorf("socket %s not inside %s", ln.Addr(), dir)
	}
}

func TestCRLFWriter(t *testing.T) {
	var b bytes.Buffer
	n, err := (&crlfWriter{w: &b}).Write([]byte("a\nb\n"))
	if err != nil || n != 4 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if b.String() != "a\r\nb\r\n" {
		t.Errorf("got %q", b.String())
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within 5s")
}

// syncBuffer is a bytes.Buffer safe for a writer goroutine and a reader.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// A configured session user that can't be resolved must refuse the session:
// falling back to the agent's identity would run it as root.
func TestSessionCredentialRefusesUnknownUser(t *testing.T) {
	_, err := (&Server{cfg: Config{User: "no-such-user-dcx"}}).sessionCredential()
	if err == nil {
		t.Fatal("unknown user must be an error")
	}

	self := strconv.Itoa(os.Geteuid())
	if cred, err := (&Server{cfg: Config{User: self}}).sessionCredential(); err != nil || cred != nil {
		t.Fatalf("own numeric UID needs no change: cred=%v err=%v", cred, err)
	}

	cred, err := (&Server{cfg: Config{User: "4242424"}}).sessionCredential()
	if err != nil || cred == nil || cred.Uid != 4242424 || cred.Gid != 4242424 {
		t.Fatalf("numeric UID without a passwd entry runs as that UID: cred=%v err=%v", cred, err)
	}
}

// A PTY session ends when its command exits, even if a background job
// still holds the terminal open (R12): copying the PTY output used to wait
// for every terminal file descriptor to close.
func TestPTYSessionEndsWithBackgroundJob(t *testing.T) {
	client, _ := startTestServer(t, nil)
	sess, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.RequestPty("xterm", 24, 80, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	sess.Stdout = &out
	done := make(chan error, 1)
	// set -m gives the job its own process group, as in an interactive
	// shell, so it survives the shell exiting with the terminal open.
	go func() { done <- sess.Run("set -m; sleep 30 & echo started") }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("session failed: %v (output %q)", err, out.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("PTY session did not end after its command exited")
	}
	if !strings.Contains(out.String(), "started") {
		t.Errorf("output before exit was lost: %q", out.String())
	}
}
