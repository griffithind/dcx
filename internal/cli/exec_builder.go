package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/env"
	"github.com/griffithind/dcx/internal/ssh/agent"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"golang.org/x/term"
)

// ExecFlags holds parsed command-line flags for exec operations.
type ExecFlags struct {
	// Command is the command and arguments to run.
	Command []string

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
	containerInfo *state.ContainerInfo
	cfg           *devcontainer.DevContainerConfig
	workspacePath string
	prober        *env.Prober
}

// NewExecBuilder creates a new exec builder.
func NewExecBuilder(containerInfo *state.ContainerInfo, cfg *devcontainer.DevContainerConfig, workspacePath string) *ExecBuilder {
	return &ExecBuilder{
		containerInfo: containerInfo,
		cfg:           cfg,
		workspacePath: workspacePath,
	}
}

// WithProber enables userEnvProbe support.
func (b *ExecBuilder) WithProber() *ExecBuilder {
	b.prober = env.NewProber()
	return b
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
		// Pass TERM and locale vars from host (aligned with OpenSSH SendEnv defaults)
		// TERM is required for terminal applications to work correctly
		if termEnv := os.Getenv("TERM"); termEnv != "" {
			args = append(args, "-e", fmt.Sprintf("TERM=%s", termEnv))
		}
		// LANG and LC_* are the default SendEnv in most SSH configs
		if lang := os.Getenv("LANG"); lang != "" {
			args = append(args, "-e", fmt.Sprintf("LANG=%s", lang))
		}
		// Pass LC_* locale variables
		for _, env := range os.Environ() {
			if len(env) > 3 && env[:3] == "LC_" {
				args = append(args, "-e", env)
			}
		}
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
			user = devcontainer.Substitute(user, &devcontainer.SubstitutionContext{
				LocalWorkspaceFolder: b.workspacePath,
			})
		}
	}

	// Add working directory
	if opts.WorkDir != "" {
		args = append(args, "-w", opts.WorkDir)
	} else if b.cfg != nil {
		workDir := devcontainer.DetermineContainerWorkspaceFolder(b.cfg, b.workspacePath)
		args = append(args, "-w", workDir)
	}

	// Add user if specified
	if user != "" {
		args = append(args, "-u", user)
		args = append(args, "-e", fmt.Sprintf("USER=%s", user))
		args = append(args, "-e", fmt.Sprintf("HOME=%s", common.GetDefaultHomeDir(user)))
	}

	// Add remoteEnv from devcontainer config (session-specific per spec)
	if b.cfg != nil {
		for k, v := range b.cfg.RemoteEnv {
			args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Add custom environment variables (opts.Env takes precedence)
	for _, env := range opts.Env {
		args = append(args, "-e", env)
	}

	return args, user
}

// Execute runs a command in the container with SSH agent support.
// This handles the full execution lifecycle including SSH proxy setup and cleanup.
func (b *ExecBuilder) Execute(ctx context.Context, opts ExecFlags) error {
	dockerArgs, user := b.BuildArgs(opts)

	// Probe user environment if configured (userEnvProbe)
	if b.prober != nil && b.cfg != nil && b.cfg.UserEnvProbe != "" {
		probeType := env.ParseProbeType(b.cfg.UserEnvProbe)
		if probeType != env.ProbeNone {
			probedEnv, err := b.prober.Probe(ctx, b.containerInfo.ID, probeType, user)
			if err != nil {
				ui.Warning("userEnvProbe failed: %v", err)
			} else {
				// Add probed environment variables (range over nil map is no-op)
				for k, v := range probedEnv {
					dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("%s=%s", k, v))
				}
			}
		}
	}

	// Setup SSH agent forwarding when available
	var agentProxy *agent.AgentProxy
	if agent.IsAvailable() {
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

// Shell opens an interactive shell in the container.
func (b *ExecBuilder) Shell(ctx context.Context, shell string) error {
	if shell == "" {
		shell = "/bin/bash"
	}

	// Use login shell for proper environment setup
	return b.Execute(ctx, ExecFlags{
		Command: []string{shell, "-l"},
		TTY:     boolPtr(true),
	})
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}
