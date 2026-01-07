// Package lifecycle handles devcontainer lifecycle hook execution.
package lifecycle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/ssh"
)

// HookRunner executes lifecycle hooks.
type HookRunner struct {
	dockerClient    *docker.Client
	containerID     string
	workspacePath   string
	cfg             *config.DevcontainerConfig
	envKey          string
	sshAgentEnabled bool
}

// NewHookRunner creates a new hook runner.
// envKey is the environment key for SSH proxy directory.
// sshAgentEnabled controls whether SSH agent forwarding is used during hook execution.
func NewHookRunner(dockerClient *docker.Client, containerID string, workspacePath string, cfg *config.DevcontainerConfig, envKey string, sshAgentEnabled bool) *HookRunner {
	return &HookRunner{
		dockerClient:    dockerClient,
		containerID:     containerID,
		workspacePath:   workspacePath,
		cfg:             cfg,
		envKey:          envKey,
		sshAgentEnabled: sshAgentEnabled,
	}
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
func (r *HookRunner) RunAllCreateHooks(ctx context.Context) error {
	// initializeCommand runs on host before anything else
	if err := r.RunInitialize(ctx); err != nil {
		return fmt.Errorf("initializeCommand failed: %w", err)
	}

	// onCreateCommand runs after container creation
	if err := r.RunOnCreate(ctx); err != nil {
		return fmt.Errorf("onCreateCommand failed: %w", err)
	}

	// updateContentCommand runs after onCreateCommand
	if err := r.RunUpdateContent(ctx); err != nil {
		return fmt.Errorf("updateContentCommand failed: %w", err)
	}

	// postCreateCommand runs after updateContentCommand
	if err := r.RunPostCreate(ctx); err != nil {
		return fmt.Errorf("postCreateCommand failed: %w", err)
	}

	// postStartCommand runs after postCreateCommand (on first start)
	if err := r.RunPostStart(ctx); err != nil {
		return fmt.Errorf("postStartCommand failed: %w", err)
	}

	return nil
}

// RunStartHooks runs hooks needed when a container is started (not first time).
func (r *HookRunner) RunStartHooks(ctx context.Context) error {
	return r.RunPostStart(ctx)
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

// executeHostCommand runs a single command on the host.
func (r *HookRunner) executeHostCommand(ctx context.Context, command string) error {
	fmt.Printf("  > %s\n", command)

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = r.workspacePath
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// executeContainerCommand runs a single command in the container.
func (r *HookRunner) executeContainerCommand(ctx context.Context, command string) error {
	fmt.Printf("  > %s\n", command)

	workspaceFolder := config.DetermineContainerWorkspaceFolder(r.cfg, r.workspacePath)

	// Apply variable substitution to remoteUser
	user := r.cfg.RemoteUser
	if user != "" {
		user = config.Substitute(user, &config.SubstitutionContext{
			LocalWorkspaceFolder: r.workspacePath,
		})
	}

	execConfig := docker.ExecConfig{
		Cmd:        []string{"sh", "-c", command},
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

		var proxyErr error
		agentProxy, proxyErr = ssh.NewAgentProxy(r.containerID, r.containerID, uid, gid)
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
// - string: single command
// - []string: array of commands (but treated as a single command line)
// - []interface{}: array of command strings
// - map[string]interface{}: named parallel commands (executed sequentially for now)
func parseCommand(command interface{}) []string {
	if command == nil {
		return nil
	}

	switch v := command.(type) {
	case string:
		return []string{v}

	case []string:
		// Array form is treated as a single command with arguments
		return []string{strings.Join(v, " ")}

	case []interface{}:
		// Could be array of strings (single command) or array of commands
		var parts []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		// Treat as single command with arguments
		return []string{strings.Join(parts, " ")}

	case map[string]interface{}:
		// Named commands - execute each one
		var cmds []string
		for name, cmd := range v {
			if cmdStr, ok := cmd.(string); ok {
				cmds = append(cmds, cmdStr)
			} else if cmdArr, ok := cmd.([]interface{}); ok {
				var parts []string
				for _, item := range cmdArr {
					if s, ok := item.(string); ok {
						parts = append(parts, s)
					}
				}
				cmds = append(cmds, strings.Join(parts, " "))
			}
			// Log the command name for verbose output
			_ = name
		}
		return cmds

	default:
		return nil
	}
}
