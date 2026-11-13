// Package lifecycle handles devcontainer lifecycle hook execution.
package lifecycle

import (
	"context"
	"fmt"
	"github.com/griffithind/dcx/internal/agentproto"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

	"github.com/griffithind/dcx/internal/report"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
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

// CommandSpec represents a parsed command that can be either a shell string
// or an exec-style array of arguments.
type CommandSpec struct {
	// Args contains the command and its arguments.
	// For shell commands, this is ["sh", "-c", "command string"].
	// For exec commands, this is the raw arguments array.
	Args []string

	// UseShell indicates whether this command should be run through a shell.
	// When true, Args[0] is the full command string to pass to sh -c.
	// When false, Args is executed directly via exec.
	UseShell bool

	// Name is an optional name for named commands (from map format).
	Name string

	// Parallel indicates this command is part of a parallel execution group.
	// Per the devcontainer spec, named commands in map format run in parallel.
	Parallel bool
}

// HookRunner executes lifecycle hooks.
type HookRunner struct {
	sshPort       int
	report        report.Reporter
	workspacePath string
	cfg           *devcontainer.DevContainerConfig
	workspaceID   string

	// Feature hooks (optional, set via SetFeatureHooks)
	featureOnCreateHooks      []features.FeatureHook
	featureUpdateContentHooks []features.FeatureHook
	featurePostCreateHooks    []features.FeatureHook
	featurePostStartHooks     []features.FeatureHook

	// Command executors; default to the real host/container execution and
	// are replaced in tests.
	execHost      func(context.Context, CommandSpec) error
	execContainer func(context.Context, CommandSpec) error
}

// NewHookRunner creates a new hook runner. Container hooks connect to the
// agent on 127.0.0.1:sshPort (0 when only host hooks run); workspaceID
// locates the workspace's pinned SSH host key.
func NewHookRunner(sshPort int, rep report.Reporter, workspacePath string, cfg *devcontainer.DevContainerConfig, workspaceID string) *HookRunner {
	r := &HookRunner{
		sshPort:       sshPort,
		report:        report.OrDiscard(rep),
		workspacePath: workspacePath,
		cfg:           cfg,
		workspaceID:   workspaceID,
	}
	r.execHost = r.executeHostCommand
	r.execContainer = r.executeContainerCommand
	return r
}

// SetFeatureHooks sets the feature lifecycle hooks to be executed.
//
// postAttachCommand is not run here: the agent runs it for every
// interactive session (see AttachSteps).
func (r *HookRunner) SetFeatureHooks(onCreate, updateContent, postCreate, postStart []features.FeatureHook) {
	r.featureOnCreateHooks = onCreate
	r.featureUpdateContentHooks = updateContent
	r.featurePostCreateHooks = postCreate
	r.featurePostStartHooks = postStart
}

// getWaitFor returns the WaitFor value from config, defaulting to updateContentCommand per spec.
func (r *HookRunner) getWaitFor() WaitFor {
	if r.cfg.WaitFor == "" {
		return WaitForUpdateContentCommand // Spec default
	}
	wf := WaitFor(r.cfg.WaitFor)
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
	cmds := ParseCommand(r.cfg.InitializeCommand)
	if len(cmds) == 0 {
		return nil
	}
	r.report.Infof("Running initializeCommand...")
	if err := r.runCommands(ctx, cmds, r.execHost); err != nil {
		return fmt.Errorf("initializeCommand failed: %w", err)
	}
	return nil
}

// RunCreateHooks runs the once-per-container hooks (onCreateCommand,
// updateContentCommand, postCreateCommand), feature hooks before the
// devcontainer.json hook for each phase. Phases recorded as done in markers
// are skipped, and each completed phase is recorded. markers may be nil.
func (r *HookRunner) RunCreateHooks(ctx context.Context, markers Markers) error {
	phases := []struct {
		name     string
		waitFor  WaitFor
		features []features.FeatureHook
		command  interface{}
	}{
		{PhaseOnCreate, WaitForOnCreateCommand, r.featureOnCreateHooks, r.cfg.OnCreateCommand},
		{PhaseUpdateContent, WaitForUpdateContentCommand, r.featureUpdateContentHooks, r.cfg.UpdateContentCommand},
		{PhasePostCreate, WaitForPostCreateCommand, r.featurePostCreateHooks, r.cfg.PostCreateCommand},
	}

	readyPrinted := false
	for _, p := range phases {
		if !readyPrinted && !r.shouldBlock(p.waitFor) {
			r.report.Infof("Container is ready (remaining hooks running...)")
			readyPrinted = true
		}
		if markers != nil && markers.Done(ctx, p.name) {
			if r.hasHooks(p.features, p.command) {
				r.report.Infof("Skipping %s (already completed)", p.name)
			}
			continue
		}
		if err := r.runPhase(ctx, p.name, p.features, p.command); err != nil {
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

// RunStartHooks runs postStartCommand (feature hooks first). It runs every
// time the container starts.
func (r *HookRunner) RunStartHooks(ctx context.Context) error {
	return r.runPhase(ctx, "postStartCommand", r.featurePostStartHooks, r.cfg.PostStartCommand)
}

func (r *HookRunner) hasHooks(featureHooks []features.FeatureHook, command interface{}) bool {
	return len(featureHooks) > 0 || len(ParseCommand(command)) > 0
}

// runPhase runs one lifecycle phase in the container: feature hooks in
// install order, then the devcontainer.json command.
func (r *HookRunner) runPhase(ctx context.Context, phase string, featureHooks []features.FeatureHook, command interface{}) error {
	for _, hook := range featureHooks {
		cmds := ParseCommand(hook.Command)
		if len(cmds) == 0 {
			continue
		}
		r.report.Infof("Running %s from feature '%s'...", phase, hook.FeatureName)
		if err := r.runCommands(ctx, cmds, r.execContainer); err != nil {
			return fmt.Errorf("feature '%s' %s failed: %w", hook.FeatureName, phase, err)
		}
	}
	cmds := ParseCommand(command)
	if len(cmds) == 0 {
		return nil
	}
	r.report.Infof("Running %s...", phase)
	if err := r.runCommands(ctx, cmds, r.execContainer); err != nil {
		return fmt.Errorf("%s failed: %w", phase, err)
	}
	return nil
}

// runCommands runs parsed commands with exec. Named (object-form) commands
// run in parallel per the spec; when one fails the shared context is
// cancelled so the others stop too. Other forms run sequentially.
func (r *HookRunner) runCommands(ctx context.Context, cmds Command, exec func(context.Context, CommandSpec) error) error {
	parallel := false
	for _, c := range cmds {
		if c.Parallel {
			parallel = true
			break
		}
	}
	if !parallel {
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
		go func(i int, c CommandSpec) {
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

// formatCommandForDisplay returns a human-readable string for displaying the command.
func formatCommandForDisplay(cmd CommandSpec) string {
	if cmd.Name != "" {
		return fmt.Sprintf("[%s] %s", cmd.Name, strings.Join(cmd.Args, " "))
	}
	return strings.Join(cmd.Args, " ")
}

// executeHostCommand runs a single command on the host.
func (r *HookRunner) executeHostCommand(ctx context.Context, cmdSpec CommandSpec) error {
	r.report.Infof("  > %s", formatCommandForDisplay(cmdSpec))

	var cmd *exec.Cmd
	if cmdSpec.UseShell {
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdSpec.Args[0])
	} else {
		cmd = exec.CommandContext(ctx, cmdSpec.Args[0], cmdSpec.Args[1:]...)
	}
	cmd.Dir = r.workspacePath
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// executeContainerCommand runs a single command in the container via SSH.
func (r *HookRunner) executeContainerCommand(ctx context.Context, cmdSpec CommandSpec) error {
	r.report.Infof("  > %s", formatCommandForDisplay(cmdSpec))

	execCmd := cmdSpec.Args
	if cmdSpec.UseShell {
		execCmd = []string{"sh", "-c", cmdSpec.Args[0]}
	}

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

// Command is a parsed lifecycle command: one entry for the string and array
// forms, one entry per name (run in parallel) for the object form.
type Command []CommandSpec

// ParseCommand parses a lifecycle command from devcontainer.json or feature
// metadata. It is the only lifecycle command parser:
//
//	"npm install"                  shell command (sh -c)
//	["npm", "install"]             exec form, arguments kept as-is
//	{"a": "cmd", "b": ["x", "y"]}  named commands, run in parallel
//
// Named commands are returned sorted by name so output is deterministic.
func ParseCommand(command interface{}) Command {
	switch v := command.(type) {
	case nil:
		return nil
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		return Command{{Args: []string{v}, UseShell: true}}
	case []string:
		if len(v) == 0 {
			return nil
		}
		return Command{{Args: v}}
	case []interface{}:
		args := stringItems(v)
		if len(args) == 0 {
			return nil
		}
		return Command{{Args: args}}
	case map[string]interface{}:
		names := make([]string, 0, len(v))
		for name := range v {
			names = append(names, name)
		}
		sort.Strings(names)
		var cmds Command
		for _, name := range names {
			switch c := v[name].(type) {
			case string:
				if strings.TrimSpace(c) != "" {
					cmds = append(cmds, CommandSpec{Args: []string{c}, UseShell: true, Name: name, Parallel: true})
				}
			case []interface{}:
				if args := stringItems(c); len(args) > 0 {
					cmds = append(cmds, CommandSpec{Args: args, Name: name, Parallel: true})
				}
			case []string:
				if len(c) > 0 {
					cmds = append(cmds, CommandSpec{Args: c, Name: name, Parallel: true})
				}
			}
		}
		return cmds
	default:
		return nil
	}
}

func stringItems(items []interface{}) []string {
	var out []string
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// AttachSteps converts postAttachCommand (feature hooks first, in install
// order, then devcontainer.json) into the steps the agent runs at the start
// of each interactive session. It uses ParseCommand, so commands mean the
// same as in every other lifecycle phase.
func AttachSteps(featureHooks []features.FeatureHook, command interface{}) []agentproto.AttachStep {
	var steps []agentproto.AttachStep
	add := func(label string, cmds Command) {
		if len(cmds) == 0 {
			return
		}
		step := agentproto.AttachStep{Label: label}
		for _, c := range cmds {
			ac := agentproto.AttachCommand{Name: c.Name}
			if c.UseShell {
				ac.Shell = c.Args[0]
			} else {
				ac.Argv = c.Args
			}
			step.Commands = append(step.Commands, ac)
		}
		steps = append(steps, step)
	}
	for _, hook := range featureHooks {
		add("feature "+hook.FeatureName, ParseCommand(hook.Command))
	}
	add("devcontainer.json", ParseCommand(command))
	return steps
}
