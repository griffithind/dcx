// Package lifecycle handles devcontainer lifecycle hook execution.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/lifecyclecmd"
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/shell"
	"github.com/griffithind/dcx/internal/ssh/hostconfig"
	"golang.org/x/term"
)

// HookRunner executes lifecycle hooks.
type HookRunner struct {
	sshAlias      string
	report        report.Reporter
	workspacePath string
	hooks         devcontainer.Hooks

	// sshOptions are added to every container hook's ssh command (the
	// shared connection, see Multiplex).
	sshOptions []string

	// Command executors; default to the real host/container execution and
	// are replaced in tests.
	execHost      func(context.Context, lifecyclecmd.Step) error
	execContainer func(context.Context, lifecyclecmd.Step) error
}

// NewHookRunner creates a new hook runner. Container hooks run with
// OpenSSH through sshAlias, the workspace's host in dcx's SSH config (""
// when only host hooks run), as users' own sessions do.
func NewHookRunner(sshAlias string, rep report.Reporter, workspacePath string, hooks devcontainer.Hooks) *HookRunner {
	r := &HookRunner{
		sshAlias:      sshAlias,
		report:        report.OrDiscard(rep),
		workspacePath: workspacePath,
		hooks:         hooks,
	}
	r.execHost = r.executeHostCommand
	r.execContainer = r.executeContainerCommand
	return r
}

// Markers records which once-per-container lifecycle phases have completed,
// so an interrupted or failed setup resumes where it stopped instead of
// re-running finished hooks (or never running the rest).
//
// Done fails closed: when it cannot tell (the container did not answer)
// it returns an error rather than "not done", which would re-run a
// once-only hook.
//
// Mark records several phases at once, so phases without hooks cost no
// extra exec.
type Markers interface {
	Done(ctx context.Context, phase string) (bool, error)
	Mark(ctx context.Context, phases ...string) error
}

// Phase names used with Markers.
const (
	PhaseOnCreate      = "onCreateCommand"
	PhaseUpdateContent = "updateContentCommand"
	PhasePostCreate    = "postCreateCommand"
)

// RunInitialize runs initializeCommand on the host. Per the spec it runs
// before the container is created or started.
func (r *HookRunner) RunInitialize(ctx context.Context) error {
	return r.runPhase(ctx, "initializeCommand", r.hooks.Initialize, r.execHost)
}

// RunCreateHooks runs the once-per-container hooks (onCreateCommand,
// updateContentCommand, postCreateCommand), each phase's hooks in order
// (image metadata, features, devcontainer.json). All of them finish before
// `dcx up` returns: waitFor (when an editor may connect) does not apply to
// a command-line tool. Phases recorded as done in markers are skipped, and
// each completed phase is recorded: one with hooks as soon as they
// finished, together with the phases without hooks before it (which are
// recorded at the end otherwise). markers may be nil.
func (r *HookRunner) RunCreateHooks(ctx context.Context, markers Markers) error {
	phases := []struct {
		name  string
		hooks []devcontainer.Hook
	}{
		{PhaseOnCreate, r.hooks.OnCreate},
		{PhaseUpdateContent, r.hooks.UpdateContent},
		{PhasePostCreate, r.hooks.PostCreate},
	}

	var pending []string // completed phases without hooks, not yet recorded
	for _, p := range phases {
		if markers != nil {
			done, err := markers.Done(ctx, p.name)
			if err != nil {
				return fmt.Errorf("cannot tell whether %s already ran: %w", p.name, err)
			}
			if done {
				if len(p.hooks) > 0 {
					r.report.Infof("Skipping %s (already completed)", p.name)
				}
				continue
			}
		}
		if err := r.runPhase(ctx, p.name, p.hooks, r.execContainer); err != nil {
			return err
		}
		pending = append(pending, p.name)
		if markers != nil && len(p.hooks) > 0 {
			if err := markers.Mark(ctx, pending...); err != nil {
				return fmt.Errorf("record %s completion: %w", p.name, err)
			}
			pending = nil
		}
	}
	if markers != nil && len(pending) > 0 {
		if err := markers.Mark(ctx, pending...); err != nil {
			return fmt.Errorf("record %s completion: %w", strings.Join(pending, ", "), err)
		}
	}
	return nil
}

// RunStartHooks runs postStartCommand. It runs every time the container
// starts.
func (r *HookRunner) RunStartHooks(ctx context.Context) error {
	return r.runPhase(ctx, "postStartCommand", r.hooks.PostStart, r.execContainer)
}

// runPhase runs one lifecycle phase's hooks in order with exec.
func (r *HookRunner) runPhase(ctx context.Context, phase string, hooks []devcontainer.Hook, exec func(context.Context, lifecyclecmd.Step) error) error {
	for _, hook := range hooks {
		origin := hook.Origin()
		if origin == "" {
			r.report.Infof("Running %s...", phase)
		} else {
			r.report.Infof("Running %s from %s...", phase, origin)
		}
		if err := r.runCommand(ctx, hook.Command, exec); err != nil {
			if origin == "" {
				return fmt.Errorf("%s failed: %w", phase, err)
			}
			return fmt.Errorf("%s %s failed: %w", origin, phase, err)
		}
	}
	return nil
}

// runCommand runs a lifecycle command with exec. Named (object-form)
// commands run in parallel per the spec; as in devcontainers/cli, every
// one runs to completion and the command fails afterwards if any failed
// (only cancelling ctx, Ctrl-C, stops them).
func (r *HookRunner) runCommand(ctx context.Context, cmd *lifecyclecmd.Command, exec func(context.Context, lifecyclecmd.Step) error) error {
	cmds := cmd.Steps()
	if !cmd.Parallel() {
		for _, c := range cmds {
			if err := exec(ctx, c); err != nil {
				return err
			}
		}
		return nil
	}

	r.report.Infof("  Running %d parallel commands...", len(cmds))
	var wg sync.WaitGroup
	errs := make([]error, len(cmds))
	for i, c := range cmds {
		wg.Add(1)
		go func(i int, c lifecyclecmd.Step) {
			defer wg.Done()
			if err := exec(ctx, c); err != nil {
				errs[i] = fmt.Errorf("[%s] %w", c.Name, err)
			}
		}(i, c)
	}
	wg.Wait()

	var first error
	for _, err := range errs {
		if err == nil {
			continue
		}
		r.report.Errorf("  %v", err)
		if first == nil {
			first = err
		}
	}
	return first
}

// executeHostCommand runs a single command on the host.
func (r *HookRunner) executeHostCommand(ctx context.Context, step lifecyclecmd.Step) error {
	r.report.Infof("  > %s", step)

	argv := stepArgv(step)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = r.workspacePath
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Multiplex makes the container hooks run until stop share one SSH
// connection (OpenSSH's ControlMaster, started by the first): a new
// connection costs about 100 ms, a session on a shared one a few. The
// control socket lives in a fresh private temporary directory (a socket
// path is limited to about 100 bytes, so not under $DCX_HOME); when even
// that path is too long, hooks connect one by one as before. stop closes
// the connection and removes the directory.
func (r *HookRunner) Multiplex() (stop func()) {
	dir, err := os.MkdirTemp("", "dcx-ssh-")
	if err != nil {
		return func() {}
	}
	socket := filepath.Join(dir, "c")
	if len(socket) > 100 {
		_ = os.RemoveAll(dir)
		return func() {}
	}
	r.sshOptions = []string{"-o", "ControlMaster=auto", "-o", "ControlPath=" + socket, "-o", "ControlPersist=30"}
	return func() {
		r.sshOptions = nil
		if _, err := os.Stat(socket); err == nil {
			args := append(r.configArgs(), "-o", "ControlPath="+socket, "-O", "exit", r.sshAlias)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = exec.CommandContext(ctx, "ssh", args...).Run()
			cancel()
		}
		_ = os.RemoveAll(dir)
	}
}

// configArgs selects dcx's SSH config when it is not the user's default.
func (r *HookRunner) configArgs() []string {
	if config := hostconfig.ConfigFileOverride(); config != "" {
		return []string{"-F", config}
	}
	return nil
}

// executeContainerCommand runs a single command in the container with
// OpenSSH. The agent gives every session the workspace's remoteEnv and
// probed user environment, so hooks see the same environment as shells.
//
// When dcx's output is a terminal the command gets a PTY (-tt), as
// devcontainers/cli runs hooks on one, sized like dcx's terminal: ssh
// can't read the size itself, because its stdin is /dev/null so that a
// hook never reads the user's typing or puts their terminal in raw mode.
// Without a terminal (CI, a pipe) there is none, so logs keep plain line
// endings and stdout and stderr stay apart. Cancelling ctx (Ctrl-C) ends
// ssh, and the agent then stops the command.
func (r *HookRunner) executeContainerCommand(ctx context.Context, step lifecyclecmd.Step) error {
	r.report.Infof("  > %s", step)
	if r.sshAlias == "" {
		return fmt.Errorf("SSH is not configured for the container")
	}
	args := append(r.configArgs(), r.sshOptions...)
	remote := shell.Join(stepArgv(step))
	if cols, rows, ok := terminalSize(); ok {
		args = append(args, "-tt")
		remote = fmt.Sprintf("stty cols %d rows %d 2>/dev/null; %s", cols, rows, remote)
	} else {
		args = append(args, "-T")
	}
	args = append(args, r.sshAlias, "--", remote)

	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 5 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if exitErr.ExitCode() == 255 {
			return fmt.Errorf("command exited with code 255 (or ssh could not connect)")
		}
		return fmt.Errorf("command exited with code %d", exitErr.ExitCode())
	}
	return err
}

// terminalSize is dcx's terminal size when its output is a terminal.
func terminalSize() (cols, rows int, ok bool) {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return 0, 0, false
	}
	cols, rows, err := term.GetSize(fd)
	if err != nil || cols <= 0 || rows <= 0 {
		cols, rows = 80, 24
	}
	return cols, rows, true
}

// stepArgv is the argv that runs step: sh -c for the shell form.
func stepArgv(step lifecyclecmd.Step) []string {
	if step.Argv != nil {
		return step.Argv
	}
	return []string{"sh", "-c", step.Shell}
}

// AttachSteps converts the postAttachCommand hooks (image metadata,
// features, devcontainer.json) into the steps the agent runs at the start
// of each interactive session.
func AttachSteps(hooks []devcontainer.Hook) []agentproto.AttachStep {
	var steps []agentproto.AttachStep
	for _, hook := range hooks {
		label := hook.Origin()
		if label == "" {
			label = string(devcontainer.HookFromConfig)
		}
		step := agentproto.AttachStep{Label: label}
		for _, c := range hook.Command.Steps() {
			step.Commands = append(step.Commands, agentproto.AttachCommand{Name: c.Name, Shell: c.Shell, Argv: c.Argv})
		}
		if len(step.Commands) > 0 {
			steps = append(steps, step)
		}
	}
	return steps
}
