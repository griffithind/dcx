// Package lifecycle handles devcontainer lifecycle hook execution.
package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/ssh"
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
}

// FeatureHook represents a lifecycle hook from a feature.
// This mirrors features.FeatureHook to avoid import cycles.
type FeatureHook struct {
	FeatureID   string
	FeatureName string
	Command     interface{}
}

// HookRunner executes lifecycle hooks.
type HookRunner struct {
	dockerClient     *docker.Client
	containerID      string
	workspacePath    string
	cfg              *config.DevcontainerConfig
	envKey           string
	sshAgentEnabled  bool
	agentPreDeployed bool

	// Feature hooks (optional, set via SetFeatureHooks)
	featureOnCreateHooks    []FeatureHook
	featurePostCreateHooks  []FeatureHook
	featurePostStartHooks   []FeatureHook
}

// NewHookRunner creates a new hook runner.
// envKey is the environment key for SSH proxy directory.
// sshAgentEnabled controls whether SSH agent forwarding is used during hook execution.
// agentPreDeployed indicates whether the agent binary was pre-deployed during 'up'.
func NewHookRunner(dockerClient *docker.Client, containerID string, workspacePath string, cfg *config.DevcontainerConfig, envKey string, sshAgentEnabled bool, agentPreDeployed bool) *HookRunner {
	return &HookRunner{
		dockerClient:     dockerClient,
		containerID:      containerID,
		workspacePath:    workspacePath,
		cfg:              cfg,
		envKey:           envKey,
		sshAgentEnabled:  sshAgentEnabled,
		agentPreDeployed: agentPreDeployed,
	}
}

// SetFeatureHooks sets the feature lifecycle hooks to be executed.
func (r *HookRunner) SetFeatureHooks(onCreate, postCreate, postStart []FeatureHook) {
	r.featureOnCreateHooks = onCreate
	r.featurePostCreateHooks = postCreate
	r.featurePostStartHooks = postStart
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
	fmt.Println("Running initializeCommand...")
	return r.runHostCommand(ctx, r.cfg.InitializeCommand)
}

// RunOnCreate runs onCreateCommand in the container.
func (r *HookRunner) RunOnCreate(ctx context.Context) error {
	if r.cfg.OnCreateCommand == nil {
		return nil
	}
	fmt.Println("Running onCreateCommand...")
	return r.runContainerCommand(ctx, r.cfg.OnCreateCommand)
}

// RunUpdateContent runs updateContentCommand in the container.
func (r *HookRunner) RunUpdateContent(ctx context.Context) error {
	if r.cfg.UpdateContentCommand == nil {
		return nil
	}
	fmt.Println("Running updateContentCommand...")
	return r.runContainerCommand(ctx, r.cfg.UpdateContentCommand)
}

// RunPostCreate runs postCreateCommand in the container.
func (r *HookRunner) RunPostCreate(ctx context.Context) error {
	if r.cfg.PostCreateCommand == nil {
		return nil
	}
	fmt.Println("Running postCreateCommand...")
	return r.runContainerCommand(ctx, r.cfg.PostCreateCommand)
}

// RunPostStart runs postStartCommand in the container.
func (r *HookRunner) RunPostStart(ctx context.Context) error {
	if r.cfg.PostStartCommand == nil {
		return nil
	}
	fmt.Println("Running postStartCommand...")
	return r.runContainerCommand(ctx, r.cfg.PostStartCommand)
}

// RunPostAttach runs postAttachCommand in the container.
func (r *HookRunner) RunPostAttach(ctx context.Context) error {
	if r.cfg.PostAttachCommand == nil {
		return nil
	}
	fmt.Println("Running postAttachCommand...")
	return r.runContainerCommand(ctx, r.cfg.PostAttachCommand)
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
				fmt.Printf("Background %s failed: %v\n", name, err)
			}
		}()
		return nil
	}

	// Log waitFor setting if not default
	if waitFor != WaitForPostStartCommand {
		fmt.Printf("Container will be ready after %s (remaining hooks run in background)\n", waitFor)
	}

	// initializeCommand runs on host before anything else
	if err := runHook(WaitForInitializeCommand, "initializeCommand", func() error {
		return r.RunInitialize(ctx)
	}); err != nil {
		return fmt.Errorf("initializeCommand failed: %w", err)
	}

	// onCreateCommand runs after container creation
	if err := runHook(WaitForOnCreateCommand, "onCreateCommand", func() error {
		if err := r.RunOnCreate(ctx); err != nil {
			return err
		}
		// Feature onCreateCommands run after devcontainer onCreateCommand
		return r.runFeatureHooks(ctx, r.featureOnCreateHooks, "onCreateCommand")
	}); err != nil {
		return fmt.Errorf("onCreateCommand failed: %w", err)
	}

	// updateContentCommand runs after onCreateCommand
	if err := runHook(WaitForUpdateContentCommand, "updateContentCommand", func() error {
		return r.RunUpdateContent(ctx)
	}); err != nil {
		return fmt.Errorf("updateContentCommand failed: %w", err)
	}

	// postCreateCommand runs after updateContentCommand
	if err := runHook(WaitForPostCreateCommand, "postCreateCommand", func() error {
		if err := r.RunPostCreate(ctx); err != nil {
			return err
		}
		// Feature postCreateCommands run after devcontainer postCreateCommand
		return r.runFeatureHooks(ctx, r.featurePostCreateHooks, "postCreateCommand")
	}); err != nil {
		return fmt.Errorf("postCreateCommand failed: %w", err)
	}

	// postStartCommand runs after postCreateCommand (on first start)
	if err := runHook(WaitForPostStartCommand, "postStartCommand", func() error {
		if err := r.RunPostStart(ctx); err != nil {
			return err
		}
		// Feature postStartCommands run after devcontainer postStartCommand
		return r.runFeatureHooks(ctx, r.featurePostStartHooks, "postStartCommand")
	}); err != nil {
		return fmt.Errorf("postStartCommand failed: %w", err)
	}

	// If we have background tasks, wait for them but don't block the user
	if waitFor != WaitForPostStartCommand {
		go func() {
			backgroundWg.Wait()
			if len(backgroundErrs) > 0 {
				fmt.Printf("Warning: %d background lifecycle hook(s) failed\n", len(backgroundErrs))
			} else {
				fmt.Println("Background lifecycle hooks completed successfully")
			}
		}()
	}

	return nil
}

// RunStartHooks runs hooks needed when a container is started (not first time).
func (r *HookRunner) RunStartHooks(ctx context.Context) error {
	if err := r.RunPostStart(ctx); err != nil {
		return err
	}

	// Feature postStartCommands run after devcontainer postStartCommand
	if err := r.runFeatureHooks(ctx, r.featurePostStartHooks, "postStartCommand"); err != nil {
		return err
	}

	return nil
}

// runFeatureHooks executes a list of feature hooks.
func (r *HookRunner) runFeatureHooks(ctx context.Context, hooks []FeatureHook, hookType string) error {
	for _, hook := range hooks {
		fmt.Printf("Running %s from feature '%s'...\n", hookType, hook.FeatureName)
		if err := r.runContainerCommand(ctx, hook.Command); err != nil {
			return fmt.Errorf("feature '%s' %s failed: %w", hook.FeatureName, hookType, err)
		}
	}
	return nil
}

// runHostCommand executes a command on the host machine.
func (r *HookRunner) runHostCommand(ctx context.Context, command interface{}) error {
	cmds := parseCommand(command)
	for _, cmd := range cmds {
		if err := r.executeHostCommand(ctx, cmd); err != nil {
			return err
		}
	}
	return nil
}

// runContainerCommand executes a command inside the container.
func (r *HookRunner) runContainerCommand(ctx context.Context, command interface{}) error {
	cmds := parseCommand(command)
	for _, cmd := range cmds {
		if err := r.executeContainerCommand(ctx, cmd); err != nil {
			return err
		}
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
	fmt.Printf("  > %s\n", formatCommandForDisplay(cmdSpec))

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
	fmt.Printf("  > %s\n", formatCommandForDisplay(cmdSpec))

	workspaceFolder := config.DetermineContainerWorkspaceFolder(r.cfg, r.workspacePath)

	// Apply variable substitution to remoteUser
	user := r.cfg.RemoteUser
	if user != "" {
		user = config.Substitute(user, &config.SubstitutionContext{
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

	execConfig := docker.ExecConfig{
		Cmd:        execCmd,
		WorkingDir: workspaceFolder,
		User:       user,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}

	// Set USER environment variable if we have a user
	if user != "" {
		execConfig.Env = append(execConfig.Env, fmt.Sprintf("USER=%s", user))
		execConfig.Env = append(execConfig.Env, fmt.Sprintf("HOME=/home/%s", user))
	}

	// Setup SSH agent forwarding if enabled
	var agentProxy *ssh.AgentProxy
	if r.sshAgentEnabled && ssh.IsAgentAvailable() {
		// Get UID/GID for the container user (use containerID for both ID and name, docker accepts either)
		uid, gid := ssh.GetContainerUserIDs(r.containerID, user)

		opts := ssh.AgentProxyOptions{SkipDeploy: r.agentPreDeployed}
		var proxyErr error
		agentProxy, proxyErr = ssh.NewAgentProxyWithOptions(r.containerID, r.containerID, uid, gid, opts)
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

	exitCode, err := r.dockerClient.Exec(ctx, r.containerID, execConfig)
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
		// Named commands - execute each one
		var cmds []CommandSpec
		for name, cmd := range v {
			if cmdStr, ok := cmd.(string); ok {
				// Named string command: shell execution
				cmds = append(cmds, CommandSpec{
					Args:     []string{cmdStr},
					UseShell: true,
					Name:     name,
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
					})
				}
			}
		}
		return cmds

	default:
		return nil
	}
}
