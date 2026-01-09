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
	"github.com/griffithind/dcx/internal/ui"
	"github.com/griffithind/dcx/internal/workspace"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var shellNoAgent bool

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open an interactive shell",
	Long: `Open an interactive shell in the running devcontainer.

By default, SSH agent forwarding is enabled if available. Use --no-agent
to disable it.

The shell used is /bin/bash if available, otherwise /bin/sh.`,
	RunE: runShell,
}

func init() {
	shellCmd.Flags().BoolVar(&shellNoAgent, "no-agent", false, "disable SSH agent forwarding")
	shellCmd.GroupID = "execution"
	rootCmd.AddCommand(shellCmd)
}

func runShell(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Initialize state manager
	stateMgr := state.NewManager(dockerClient)
	envKey := workspace.ComputeID(workspacePath)

	// Check current state
	currentState, containerInfo, err := stateMgr.GetState(ctx, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	switch currentState {
	case state.StateAbsent:
		return fmt.Errorf("no devcontainer found; run 'dcx up' first")
	case state.StateCreated:
		return fmt.Errorf("devcontainer is not running; run 'dcx start' first")
	case state.StateBroken:
		return fmt.Errorf("devcontainer is in broken state; run 'dcx up --recreate'")
	case state.StateStale:
		fmt.Fprintln(os.Stderr, "Warning: devcontainer is stale (config changed)")
	}

	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Load config to get user and workspace folder
	cfg, _, _ := config.Load(workspacePath, configPath)

	// Build docker exec command
	dockerArgs := []string{"exec"}

	// Add TTY flags if we have a terminal
	if term.IsTerminal(int(os.Stdin.Fd())) {
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
	if !shellNoAgent && ssh.IsAgentAvailable() {
		// Get UID/GID for the container user
		uid, gid := ssh.GetContainerUserIDs(containerInfo.Name, user)

		agentProxy, err = ssh.NewAgentProxy(containerInfo.ID, containerInfo.Name, uid, gid)
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

	// Add container name and shell command
	dockerArgs = append(dockerArgs, containerInfo.Name)
	dockerArgs = append(dockerArgs, "/bin/bash", "-l")

	// Run docker exec (don't replace process so agent can capture output)
	dockerCmd := exec.Command("docker", dockerArgs...)
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
		return fmt.Errorf("shell failed: %w", err)
	}

	return nil
}
