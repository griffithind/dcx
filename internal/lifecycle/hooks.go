// Package lifecycle handles devcontainer lifecycle hook execution.
package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/ssh/agent"
	"github.com/griffithind/dcx/internal/ui"
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
	dockerClient    *container.DockerClient
	containerID     string
	workspacePath   string
	cfg             *devcontainer.DevContainerConfig
	workspaceID     string
	sshAgentEnabled bool

	// Feature hooks (optional, set via SetFeatureHooks)
	featureOnCreateHooks      []features.FeatureHook
	featureUpdateContentHooks []features.FeatureHook
	featurePostCreateHooks    []features.FeatureHook
	featurePostStartHooks     []features.FeatureHook
	featurePostAttachHooks    []features.FeatureHook
}

// NewHookRunner creates a new hook runner.
// workspaceID is the environment key for SSH proxy directory.
// sshAgentEnabled controls whether SSH agent forwarding is used during hook execution.
func NewHookRunner(dockerClient *container.DockerClient, containerID string, workspacePath string, cfg *devcontainer.DevContainerConfig, workspaceID string, sshAgentEnabled bool) *HookRunner {
	return &HookRunner{
		dockerClient:    dockerClient,
		containerID:     containerID,
		workspacePath:   workspacePath,
		cfg:             cfg,
		workspaceID:     workspaceID,
		sshAgentEnabled: sshAgentEnabled,
	}
}

// SetFeatureHooks sets the feature lifecycle hooks to be executed.
func (r *HookRunner) SetFeatureHooks(onCreate, updateContent, postCreate, postStart, postAttach []features.FeatureHook) {
	r.featureOnCreateHooks = onCreate
	r.featureUpdateContentHooks = updateContent
	r.featurePostCreateHooks = postCreate
	r.featurePostStartHooks = postStart
	r.featurePostAttachHooks = postAttach
}

// getWaitFor returns the WaitFor value from config, defaulting to postStartCommand.
func (r *HookRunner) getWaitFor() WaitFor {
	if r.cfg.WaitFor == "" {
		return WaitForPostStartCommand
	}
	wf := WaitFor(r.cfg.WaitFor)
	if _, ok := waitForOrder[wf]; !ok {
		// Invalid value, use default
		return WaitForPostStartCommand
	}
	return wf
}

// shouldBlock returns true if the given command should block (wait for completion).
func (r *HookRunner) shouldBlock(cmd WaitFor) bool {
	waitFor := r.getWaitFor()
	return waitForOrder[cmd] <= waitForOrder[waitFor]
}

// RunInitialize runs initializeCommand on the host.
func (r *HookRunner) RunInitialize(ctx context.Context) error {
	if r.cfg.InitializeCommand == nil {
		return nil
	}
	ui.Println("Running initializeCommand...")
	return r.runHostCommand(ctx, r.cfg.InitializeCommand)
}

// RunOnCreate runs onCreateCommand in the container.
func (r *HookRunner) RunOnCreate(ctx context.Context) error {
	if r.cfg.OnCreateCommand == nil {
		return nil
	}
	ui.Println("Running onCreateCommand...")
	return r.runContainerCommand(ctx, r.cfg.OnCreateCommand)
}

// RunUpdateContent runs updateContentCommand in the container.
func (r *HookRunner) RunUpdateContent(ctx context.Context) error {
	if r.cfg.UpdateContentCommand == nil {
		return nil
	}
	ui.Println("Running updateContentCommand...")
	return r.runContainerCommand(ctx, r.cfg.UpdateContentCommand)
}

// RunPostCreate runs postCreateCommand in the container.
func (r *HookRunner) RunPostCreate(ctx context.Context) error {
	if r.cfg.PostCreateCommand == nil {
		return nil
	}
	ui.Println("Running postCreateCommand...")
	return r.runContainerCommand(ctx, r.cfg.PostCreateCommand)
}

// RunPostStart runs postStartCommand in the container.
func (r *HookRunner) RunPostStart(ctx context.Context) error {
	if r.cfg.PostStartCommand == nil {
		return nil
	}
	ui.Println("Running postStartCommand...")
	return r.runContainerCommand(ctx, r.cfg.PostStartCommand)
}

// RunPostAttach runs postAttachCommand in the container.
// Per spec: feature hooks run BEFORE devcontainer hooks.
func (r *HookRunner) RunPostAttach(ctx context.Context) error {
	// Feature postAttachCommands run before devcontainer postAttachCommand
	if err := r.runFeatureHooks(ctx, r.featurePostAttachHooks, "postAttachCommand"); err != nil {
		return err
	}

	if r.cfg.PostAttachCommand != nil {
		ui.Println("Running postAttachCommand...")
		if err := r.runContainerCommand(ctx, r.cfg.PostAttachCommand); err != nil {
			return err
		}
	}

	return nil
}

// RunAllCreateHooks runs all hooks needed when a container is first created.
// Commands are run in order, but commands after the waitFor point run in the background.
func (r *HookRunner) RunAllCreateHooks(ctx context.Context) error {
	waitFor := r.getWaitFor()
	var backgroundWg sync.WaitGroup
	var backgroundErrs []error
	var backgroundMu sync.Mutex

	// Helper to run a hook either blocking or in background based on waitFor
	runHook := func(hookType WaitFor, name string, fn func() error) error {
		if r.shouldBlock(hookType) {
			// Run synchronously and return error immediately
			return fn()
		}
		// Run in background
		backgroundWg.Add(1)
		go func() {
			defer backgroundWg.Done()
			if err := fn(); err != nil {
				backgroundMu.Lock()
				backgroundErrs = append(backgroundErrs, fmt.Errorf("%s: %w", name, err))
				backgroundMu.Unlock()
				ui.Warning("Background %s failed: %v", name, err)
			}
		}()
		return nil
	}

	// Log waitFor setting if not default
	if waitFor != WaitForPostStartCommand {
		ui.Printf("Container will be ready after %s (remaining hooks run in background)", waitFor)
	}

	// initializeCommand runs on host before anything else
	if err := runHook(WaitForInitializeCommand, "initializeCommand", func() error {
		return r.RunInitialize(ctx)
	}); err != nil {
		return fmt.Errorf("initializeCommand failed: %w", err)
	}

	// onCreateCommand runs after container creation
	// Per spec: feature hooks run BEFORE devcontainer hooks
	if err := runHook(WaitForOnCreateCommand, "onCreateCommand", func() error {
		if err := r.runFeatureHooks(ctx, r.featureOnCreateHooks, "onCreateCommand"); err != nil {
			return err
		}
		return r.RunOnCreate(ctx)
	}); err != nil {
		return fmt.Errorf("onCreateCommand failed: %w", err)
	}

	// updateContentCommand runs after onCreateCommand
	// Per spec: feature hooks run BEFORE devcontainer hooks
	if err := runHook(WaitForUpdateContentCommand, "updateContentCommand", func() error {
		if err := r.runFeatureHooks(ctx, r.featureUpdateContentHooks, "updateContentCommand"); err != nil {
			return err
		}
		return r.RunUpdateContent(ctx)
	}); err != nil {
		return fmt.Errorf("updateContentCommand failed: %w", err)
	}

	// postCreateCommand runs after updateContentCommand
	// Per spec: feature hooks run BEFORE devcontainer hooks
	if err := runHook(WaitForPostCreateCommand, "postCreateCommand", func() error {
		if err := r.runFeatureHooks(ctx, r.featurePostCreateHooks, "postCreateCommand"); err != nil {
			return err
		}
		return r.RunPostCreate(ctx)
	}); err != nil {
		return fmt.Errorf("postCreateCommand failed: %w", err)
	}

	// postStartCommand runs after postCreateCommand (on first start)
	// Per spec: feature hooks run BEFORE devcontainer hooks
	if err := runHook(WaitForPostStartCommand, "postStartCommand", func() error {
		if err := r.runFeatureHooks(ctx, r.featurePostStartHooks, "postStartCommand"); err != nil {
			return err
		}
		return r.RunPostStart(ctx)
	}); err != nil {
		return fmt.Errorf("postStartCommand failed: %w", err)
	}

	// If we have background tasks, wait for them but don't block the user
	if waitFor != WaitForPostStartCommand {
		go func() {
			backgroundWg.Wait()
			if len(backgroundErrs) > 0 {
				ui.Warning("%d background lifecycle hook(s) failed", len(backgroundErrs))
			} else {
				ui.Println("Background lifecycle hooks completed successfully")
			}
		}()
	}

	return nil
}

// RunStartHooks runs hooks needed when a container is started (not first time).
// Per spec: feature hooks run BEFORE devcontainer hooks.
func (r *HookRunner) RunStartHooks(ctx context.Context) error {
	// Feature postStartCommands run before devcontainer postStartCommand
	if err := r.runFeatureHooks(ctx, r.featurePostStartHooks, "postStartCommand"); err != nil {
		return err
	}

	if err := r.RunPostStart(ctx); err != nil {
		return err
	}

	return nil
}

// runFeatureHooks executes a list of feature hooks.
func (r *HookRunner) runFeatureHooks(ctx context.Context, hooks []features.FeatureHook, hookType string) error {
	for _, hook := range hooks {
		ui.Printf("Running %s from feature '%s'...", hookType, hook.FeatureName)
		if err := r.runContainerCommand(ctx, hook.Command); err != nil {
			return fmt.Errorf("feature '%s' %s failed: %w", hook.FeatureName, hookType, err)
		}
	}
	return nil
}

// runHostCommand executes a command on the host machine.
// Per spec, named commands (map format) run in parallel.
func (r *HookRunner) runHostCommand(ctx context.Context, command interface{}) error {
	cmds := parseCommand(command)
	if len(cmds) == 0 {
		return nil
	}

	// Check if any commands are parallel (map commands)
	hasParallel := false
	for _, cmd := range cmds {
		if cmd.Parallel {
			hasParallel = true
			break
		}
	}

	// Sequential execution for non-parallel commands
	if !hasParallel {
		for _, cmd := range cmds {
			if err := r.executeHostCommand(ctx, cmd); err != nil {
				return err
			}
		}
		return nil
	}

	// Parallel execution for map commands with context cancellation
	// Per spec, if one parallel command fails, cancel the others
	ui.Printf("  Running %d parallel commands...", len(cmds))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(cmds))

	for _, cmd := range cmds {
		cmd := cmd // capture for goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return // Context cancelled, stop execution
			default:
				if err := r.executeHostCommand(ctx, cmd); err != nil {
					errCh <- fmt.Errorf("[%s] %w", cmd.Name, err)
					cancel() // Cancel other parallel commands
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// Collect errors
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		// Return first error, but log all
		for _, err := range errs {
			ui.Error("  %v", err)
		}
		return errs[0]
	}

	return nil
}

// runContainerCommand executes a command inside the container.
// Per spec, named commands (map format) run in parallel.
func (r *HookRunner) runContainerCommand(ctx context.Context, command interface{}) error {
	cmds := parseCommand(command)
	if len(cmds) == 0 {
		return nil
	}

	// Check if any commands are parallel (map commands)
	hasParallel := false
	for _, cmd := range cmds {
		if cmd.Parallel {
			hasParallel = true
			break
		}
	}

	// Sequential execution for non-parallel commands
	if !hasParallel {
		for _, cmd := range cmds {
			if err := r.executeContainerCommand(ctx, cmd); err != nil {
				return err
			}
		}
		return nil
	}

	// Parallel execution for map commands with context cancellation
	// Per spec, if one parallel command fails, cancel the others
	ui.Printf("  Running %d parallel commands...", len(cmds))
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, len(cmds))

	for _, cmd := range cmds {
		cmd := cmd // capture for goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case <-ctx.Done():
				return // Context cancelled, stop execution
			default:
				if err := r.executeContainerCommand(ctx, cmd); err != nil {
					errCh <- fmt.Errorf("[%s] %w", cmd.Name, err)
					cancel() // Cancel other parallel commands
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	// Collect errors
	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		// Return first error, but log all
		for _, err := range errs {
			ui.Error("  %v", err)
		}
		return errs[0]
	}

	return nil
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
	ui.Printf("  > %s", formatCommandForDisplay(cmdSpec))

	var cmd *exec.Cmd
	if cmdSpec.UseShell {
		// Shell command: pass through sh -c
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdSpec.Args[0])
	} else {
		// Exec command: execute directly with args
		cmd = exec.CommandContext(ctx, cmdSpec.Args[0], cmdSpec.Args[1:]...)
	}
	cmd.Dir = r.workspacePath
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// executeContainerCommand runs a single command in the container.
func (r *HookRunner) executeContainerCommand(ctx context.Context, cmdSpec CommandSpec) error {
	ui.Printf("  > %s", formatCommandForDisplay(cmdSpec))

	workspaceFolder := devcontainer.DetermineContainerWorkspaceFolder(r.cfg, r.workspacePath)

	// Apply variable substitution to remoteUser
	user := r.cfg.RemoteUser
	if user != "" {
		user = devcontainer.Substitute(user, &devcontainer.SubstitutionContext{
			LocalWorkspaceFolder: r.workspacePath,
		})
	}

	// Build the command to execute
	var execCmd []string
	if cmdSpec.UseShell {
		// Shell command: wrap with sh -c
		execCmd = []string{"sh", "-c", cmdSpec.Args[0]}
	} else {
		// Exec command: use args directly
		execCmd = cmdSpec.Args
	}

	execConfig := container.ExecConfig{
		ContainerID: r.containerID,
		Cmd:         execCmd,
		WorkingDir:  workspaceFolder,
		User:        user,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
	}

	// Set USER environment variable if we have a user
	if user != "" {
		execConfig.Env = append(execConfig.Env, fmt.Sprintf("USER=%s", user))
		execConfig.Env = append(execConfig.Env, fmt.Sprintf("HOME=/home/%s", user))
	}

	// Setup SSH agent forwarding if enabled
	var agentProxy *agent.AgentProxy
	if r.sshAgentEnabled && agent.IsAvailable() {
		// Get UID/GID for the container user (use containerID for both ID and name, docker accepts either)
		uid, gid := agent.GetContainerUserIDs(r.containerID, user)

		var proxyErr error
		agentProxy, proxyErr = agent.NewAgentProxy(r.containerID, r.containerID, uid, gid)
		if proxyErr == nil {
			socketPath, startErr := agentProxy.Start()
			if startErr == nil {
				execConfig.Env = append(execConfig.Env, fmt.Sprintf("SSH_AUTH_SOCK=%s", socketPath))
			}
		}
	}
	defer func() {
		if agentProxy != nil {
			agentProxy.Stop()
		}
	}()

	exitCode, err := container.Exec(ctx, r.dockerClient.APIClient(), execConfig)
	if err != nil {
		return err
	}

	if exitCode != 0 {
		return fmt.Errorf("command exited with code %d", exitCode)
	}

	return nil
}

// parseCommand parses a command specification into individual commands.
// Commands can be:
// - string: single shell command (executed via sh -c)
// - []string: exec-style command with arguments (executed directly)
// - []interface{}: exec-style command with arguments (executed directly)
// - map[string]interface{}: named parallel commands (executed sequentially for now)
//
// Per the devcontainer spec:
// - String format: "npm install" -> executed as shell command
// - Array format: ["npm", "install"] -> executed directly with exec semantics
func parseCommand(command interface{}) []CommandSpec {
	if command == nil {
		return nil
	}

	switch v := command.(type) {
	case string:
		// String command: execute via shell
		return []CommandSpec{{
			Args:     []string{v},
			UseShell: true,
		}}

	case []string:
		// Array of strings: exec-style command with arguments
		if len(v) == 0 {
			return nil
		}
		return []CommandSpec{{
			Args:     v,
			UseShell: false,
		}}

	case []interface{}:
		// Array of interface{}: exec-style command with arguments
		var args []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				args = append(args, s)
			}
		}
		if len(args) == 0 {
			return nil
		}
		return []CommandSpec{{
			Args:     args,
			UseShell: false,
		}}

	case map[string]interface{}:
		// Named commands - per spec, these run in parallel
		var cmds []CommandSpec
		for name, cmd := range v {
			if cmdStr, ok := cmd.(string); ok {
				// Named string command: shell execution
				cmds = append(cmds, CommandSpec{
					Args:     []string{cmdStr},
					UseShell: true,
					Name:     name,
					Parallel: true, // Map commands run in parallel per spec
				})
			} else if cmdArr, ok := cmd.([]interface{}); ok {
				// Named array command: exec-style
				var args []string
				for _, item := range cmdArr {
					if s, ok := item.(string); ok {
						args = append(args, s)
					}
				}
				if len(args) > 0 {
					cmds = append(cmds, CommandSpec{
						Args:     args,
						UseShell: false,
						Name:     name,
						Parallel: true, // Map commands run in parallel per spec
					})
				}
			}
		}
		return cmds

	default:
		return nil
	}
}
