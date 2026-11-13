// Package lifecycle handles devcontainer lifecycle hook execution.
package lifecycle

import (
	"context"
	"fmt"
	"github.com/griffithind/dcx/internal/agentproto"
	"os"
	"os/exec"
	"sync"

	"github.com/griffithind/dcx/internal/report"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/lifecyclecmd"
	sshclient "github.com/griffithind/dcx/internal/ssh/client"
)

// WaitFor represents the lifecycle command to wait for before considering
// the container ready. Commands after this point run in the background.
type WaitFor string

const (
	// WaitForInitializeCommand waits only for initializeCommand.
	WaitForInitializeCommand WaitFor = "initializeCommand"
	// WaitForOnCreateCommand waits for onCreateCommand (and earlier).
	WaitForOnCreateCommand WaitFor = "onCreateCommand"
	// WaitForUpdateContentCommand waits for updateContentCommand (and earlier).
	WaitForUpdateContentCommand WaitFor = "updateContentCommand"
	// WaitForPostCreateCommand waits for postCreateCommand (and earlier).
	WaitForPostCreateCommand WaitFor = "postCreateCommand"
	// WaitForPostStartCommand waits for all commands (default behavior).
	WaitForPostStartCommand WaitFor = "postStartCommand"
)

// waitForOrder defines the order of lifecycle commands for comparison.
var waitForOrder = map[WaitFor]int{
	WaitForInitializeCommand:    0,
	WaitForOnCreateCommand:      1,
	WaitForUpdateContentCommand: 2,
	WaitForPostCreateCommand:    3,
	WaitForPostStartCommand:     4,
}

// HookRunner executes lifecycle hooks.
type HookRunner struct {
	sshPort       int
	report        report.Reporter
	workspacePath string
	hooks         devcontainer.Hooks
	workspaceID   string

	// Command executors; default to the real host/container execution and
	// are replaced in tests.
	execHost      func(context.Context, lifecyclecmd.Step) error
	execContainer func(context.Context, lifecyclecmd.Step) error
}

// NewHookRunner creates a new hook runner. Container hooks connect to the
// agent on 127.0.0.1:sshPort (0 when only host hooks run); workspaceID
// locates the workspace's pinned SSH host key.
func NewHookRunner(sshPort int, rep report.Reporter, workspacePath string, hooks devcontainer.Hooks, workspaceID string) *HookRunner {
	r := &HookRunner{
		sshPort:       sshPort,
		report:        report.OrDiscard(rep),
		workspacePath: workspacePath,
		hooks:         hooks,
		workspaceID:   workspaceID,
	}
	r.execHost = r.executeHostCommand
	r.execContainer = r.executeContainerCommand
	return r
}

// getWaitFor returns the WaitFor value from config, defaulting to updateContentCommand per spec.
func (r *HookRunner) getWaitFor() WaitFor {
	if r.hooks.WaitFor == "" {
		return WaitForUpdateContentCommand // Spec default
	}
	wf := WaitFor(r.hooks.WaitFor)
	if _, ok := waitForOrder[wf]; !ok {
		// Invalid value, use default
		return WaitForUpdateContentCommand
	}
	return wf
}

// shouldBlock returns true if the given command should block (wait for completion).
func (r *HookRunner) shouldBlock(cmd WaitFor) bool {
	waitFor := r.getWaitFor()
	return waitForOrder[cmd] <= waitForOrder[waitFor]
}

// Markers records which once-per-container lifecycle phases have completed,
// so an interrupted or failed setup resumes where it stopped instead of
// re-running finished hooks (or never running the rest).
type Markers interface {
	Done(ctx context.Context, phase string) bool
	Mark(ctx context.Context, phase string) error
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
// (image metadata, features, devcontainer.json). Phases recorded as done in markers
// are skipped, and each completed phase is recorded. markers may be nil.
func (r *HookRunner) RunCreateHooks(ctx context.Context, markers Markers) error {
	phases := []struct {
		name    string
		waitFor WaitFor
		hooks   []devcontainer.Hook
	}{
		{PhaseOnCreate, WaitForOnCreateCommand, r.hooks.OnCreate},
		{PhaseUpdateContent, WaitForUpdateContentCommand, r.hooks.UpdateContent},
		{PhasePostCreate, WaitForPostCreateCommand, r.hooks.PostCreate},
	}

	readyPrinted := false
	for _, p := range phases {
		if !readyPrinted && !r.shouldBlock(p.waitFor) {
			r.report.Infof("Container is ready (remaining hooks running...)")
			readyPrinted = true
		}
		if markers != nil && markers.Done(ctx, p.name) {
			if len(p.hooks) > 0 {
				r.report.Infof("Skipping %s (already completed)", p.name)
			}
			continue
		}
		if err := r.runPhase(ctx, p.name, p.hooks, r.execContainer); err != nil {
			return err
		}
		if markers != nil {
			if err := markers.Mark(ctx, p.name); err != nil {
				return fmt.Errorf("record %s completion: %w", p.name, err)
			}
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
// commands run in parallel per the spec; when one fails the shared context
// is cancelled so the others stop too.
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
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errs := make([]error, len(cmds))
	for i, c := range cmds {
		wg.Add(1)
		go func(i int, c lifecyclecmd.Step) {
			defer wg.Done()
			if err := exec(ctx, c); err != nil {
				errs[i] = fmt.Errorf("[%s] %w", c.Name, err)
				cancel()
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

// executeContainerCommand runs a single command in the container via SSH.
func (r *HookRunner) executeContainerCommand(ctx context.Context, step lifecyclecmd.Step) error {
	r.report.Infof("  > %s", step)
	execCmd := stepArgv(step)

	// The agent gives every session the workspace's remoteEnv and probed
	// user environment, so hooks see the same environment as shells.
	exitCode, err := sshclient.Run(ctx, sshclient.RunOptions{
		Port:        r.sshPort,
		WorkspaceID: r.workspaceID,
		Command:     execCmd,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("command exited with code %d", exitCode)
	}
	return nil
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
