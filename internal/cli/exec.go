package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var execNoAgent bool

var execCmd = &cobra.Command{
	Use:   "exec [--no-agent] -- <command> [args...]",
	Short: "Run a command in the container",
	Long: `Run a command inside the running devcontainer.

By default, SSH agent forwarding is enabled if available. Use --no-agent
to disable it.

Examples:
  dcx exec -- npm install
  dcx exec -- ls -la /workspace
  dcx exec -- git clone git@github.com:user/repo.git
  dcx exec --no-agent -- bash -c "echo hello"`,
	RunE: runExec,
	// Args after "--" are passed directly to the command
	Args: cobra.ArbitraryArgs,
}

func runExec(cmd *cobra.Command, args []string) error {
	// With cobra handling flags, args after "--" are passed directly
	if len(args) == 0 {
		return fmt.Errorf("no command specified; usage: dcx exec [--no-agent] -- <command> [args...]")
	}
	execArgs := args

	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Initialize state manager
	stateMgr := state.NewManager(dockerClient)
	envKey := state.ComputeEnvKey(workspacePath)

	// Check current state
	currentState, containerInfo, err := stateMgr.GetState(ctx, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	switch currentState {
	case state.StateAbsent:
		return fmt.Errorf("no environment found; run 'dcx up' first")
	case state.StateCreated:
		return fmt.Errorf("environment is not running; run 'dcx start' first")
	case state.StateBroken:
		return fmt.Errorf("environment is in broken state; run 'dcx up --recreate'")
	case state.StateStale:
		fmt.Fprintln(os.Stderr, "Warning: environment is stale (config changed)")
	}

	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Load config to get user and workspace folder
	cfg, _, _ := config.Load(workspacePath, configPath)

	// Build docker exec command
	dockerArgs := []string{"exec"}

	// Check if we have a TTY
	isTTY := term.IsTerminal(int(os.Stdin.Fd()))
	if isTTY {
		dockerArgs = append(dockerArgs, "-it")
	} else {
		dockerArgs = append(dockerArgs, "-i")
	}

	// Add working directory and user
	var user string
	if cfg != nil {
		workDir := config.DetermineContainerWorkspaceFolder(cfg, workspacePath)
		dockerArgs = append(dockerArgs, "-w", workDir)

		// Add user if specified
		user = cfg.RemoteUser
		if user == "" {
			user = cfg.ContainerUser
		}
		if user != "" {
			user = config.Substitute(user, &config.SubstitutionContext{
				LocalWorkspaceFolder: workspacePath,
			})
			dockerArgs = append(dockerArgs, "-u", user)
			// Set USER and HOME env vars
			dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("USER=%s", user))
			dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("HOME=/home/%s", user))
		}
	}

	// Setup SSH agent forwarding if enabled
	var agentProxy *ssh.AgentProxy
	if !execNoAgent && ssh.IsAgentAvailable() {
		// Get UID/GID for the container user
		uid, gid := ssh.GetContainerUserIDs(containerInfo.Name, user)

		// Skip deployment since binary is pre-deployed during 'up'
		opts := ssh.AgentProxyOptions{SkipDeploy: true}
		agentProxy, err = ssh.NewAgentProxyWithOptions(containerInfo.ID, containerInfo.Name, uid, gid, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: SSH agent proxy setup failed: %v\n", err)
		} else {
			socketPath, startErr := agentProxy.Start()
			if startErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: SSH agent proxy start failed: %v\n", startErr)
			} else {
				dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("SSH_AUTH_SOCK=%s", socketPath))
			}
		}
	}

	// Add container name and command
	dockerArgs = append(dockerArgs, containerInfo.Name)
	dockerArgs = append(dockerArgs, execArgs...)

	// Run docker exec (don't replace process so agent can capture output)
	dockerCmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	dockerCmd.Stdin = os.Stdin
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr

	err = dockerCmd.Run()

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

func init() {
	execCmd.Flags().BoolVar(&execNoAgent, "no-agent", false, "disable SSH agent forwarding")
	rootCmd.AddCommand(execCmd)
}
