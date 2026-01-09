package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/containerstate"
	"github.com/griffithind/dcx/internal/ssh/agent"
	"github.com/griffithind/dcx/internal/ui"
	"golang.org/x/term"
)

// ExecFlags holds parsed command-line flags for exec operations.
type ExecFlags struct {
	// Command is the command and arguments to run.
	Command []string

	// EnableSSHAgent enables SSH agent forwarding if available.
	EnableSSHAgent bool

	// User overrides the container user.
	User string

	// WorkDir overrides the working directory.
	WorkDir string

	// Env contains additional environment variables.
	Env []string

	// TTY forces TTY allocation. If nil, auto-detects.
	TTY *bool
}

// ExecBuilder builds and executes docker exec commands.
// It consolidates the common exec pattern used in exec, shell, and run commands.
type ExecBuilder struct {
	containerInfo *containerstate.ContainerInfo
	cfg           *config.DevContainerConfig
	workspacePath string
}

// NewExecBuilder creates a new exec builder.
func NewExecBuilder(containerInfo *containerstate.ContainerInfo, cfg *config.DevContainerConfig, workspacePath string) *ExecBuilder {
	return &ExecBuilder{
		containerInfo: containerInfo,
		cfg:           cfg,
		workspacePath: workspacePath,
	}
}

// BuildArgs constructs docker exec arguments based on options.
// Returns the args slice and the resolved user.
func (b *ExecBuilder) BuildArgs(opts ExecFlags) ([]string, string) {
	args := []string{"exec"}

	// TTY detection
	isTTY := false
	if opts.TTY != nil {
		isTTY = *opts.TTY
	} else {
		isTTY = term.IsTerminal(int(os.Stdin.Fd()))
	}

	if isTTY {
		args = append(args, "-it")
	} else {
		args = append(args, "-i")
	}

	// Determine user
	user := opts.User
	if user == "" && b.cfg != nil {
		user = b.cfg.RemoteUser
		if user == "" {
			user = b.cfg.ContainerUser
		}
		if user != "" {
			user = config.Substitute(user, &config.SubstitutionContext{
				LocalWorkspaceFolder: b.workspacePath,
			})
		}
	}

	// Add working directory
	if opts.WorkDir != "" {
		args = append(args, "-w", opts.WorkDir)
	} else if b.cfg != nil {
		workDir := config.DetermineContainerWorkspaceFolder(b.cfg, b.workspacePath)
		args = append(args, "-w", workDir)
	}

	// Add user if specified
	if user != "" {
		args = append(args, "-u", user)
		args = append(args, "-e", fmt.Sprintf("USER=%s", user))
		args = append(args, "-e", fmt.Sprintf("HOME=/home/%s", user))
	}

	// Add custom environment variables
	for _, env := range opts.Env {
		args = append(args, "-e", env)
	}

	return args, user
}

// Execute runs a command in the container with optional SSH agent support.
// This handles the full execution lifecycle including SSH proxy setup and cleanup.
func (b *ExecBuilder) Execute(ctx context.Context, opts ExecFlags) error {
	dockerArgs, user := b.BuildArgs(opts)

	// Setup SSH agent forwarding if enabled
	var agentProxy *agent.AgentProxy
	if opts.EnableSSHAgent && agent.IsAvailable() {
		uid, gid := agent.GetContainerUserIDs(b.containerInfo.Name, user)
		var err error
		agentProxy, err = agent.NewAgentProxy(b.containerInfo.ID, b.containerInfo.Name, uid, gid)
		if err != nil {
			ui.Warning("SSH agent proxy setup failed: %v", err)
		} else {
			socketPath, startErr := agentProxy.Start()
			if startErr != nil {
				ui.Warning("SSH agent proxy start failed: %v", startErr)
			} else {
				dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("SSH_AUTH_SOCK=%s", socketPath))
			}
		}
	}

	// Add container name and command
	dockerArgs = append(dockerArgs, b.containerInfo.Name)
	dockerArgs = append(dockerArgs, opts.Command...)

	// Run docker exec
	dockerCmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	dockerCmd.Stdin = os.Stdin
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr

	err := dockerCmd.Run()

	// Clean up SSH agent proxy
	if agentProxy != nil {
		agentProxy.Stop()
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("exec failed: %w", err)
	}

	return nil
}

// ExecuteWithOutput runs a command and returns its output instead of streaming.
func (b *ExecBuilder) ExecuteWithOutput(ctx context.Context, opts ExecFlags) ([]byte, error) {
	dockerArgs, user := b.BuildArgs(opts)

	// Setup SSH agent forwarding if enabled
	var agentProxy *agent.AgentProxy
	if opts.EnableSSHAgent && agent.IsAvailable() {
		uid, gid := agent.GetContainerUserIDs(b.containerInfo.Name, user)
		var err error
		agentProxy, err = agent.NewAgentProxy(b.containerInfo.ID, b.containerInfo.Name, uid, gid)
		if err != nil {
			ui.Warning("SSH agent proxy setup failed: %v", err)
		} else {
			socketPath, startErr := agentProxy.Start()
			if startErr != nil {
				ui.Warning("SSH agent proxy start failed: %v", startErr)
			} else {
				dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("SSH_AUTH_SOCK=%s", socketPath))
			}
		}
	}

	// Add container name and command
	dockerArgs = append(dockerArgs, b.containerInfo.Name)
	dockerArgs = append(dockerArgs, opts.Command...)

	// Run docker exec and capture output
	dockerCmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	output, err := dockerCmd.CombinedOutput()

	// Clean up SSH agent proxy
	if agentProxy != nil {
		agentProxy.Stop()
	}

	return output, err
}

// Shell opens an interactive shell in the container.
func (b *ExecBuilder) Shell(ctx context.Context, shell string, enableSSHAgent bool) error {
	if shell == "" {
		shell = "/bin/bash"
	}

	// Use login shell for proper environment setup
	return b.Execute(ctx, ExecFlags{
		Command:        []string{shell, "-l"},
		EnableSSHAgent: enableSSHAgent,
		TTY:            boolPtr(true),
	})
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}
