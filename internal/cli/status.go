package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show devcontainer status",
	Long: `Show the current state of the devcontainer environment.

This command queries Docker for containers managed by dcx and displays
their current state (ABSENT, CREATED, RUNNING, STALE, or BROKEN).

This is an offline-safe command that does not require network access.`,
	RunE: runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Load dcx.json configuration (optional)
	dcxCfg, _ := config.LoadDcxConfig(workspacePath)

	// Get project name from dcx.json
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = state.SanitizeProjectName(dcxCfg.Name)
	}

	// Initialize state manager
	stateMgr := state.NewManager(dockerClient)

	// Compute env key from workspace
	envKey := state.ComputeEnvKey(workspacePath)

	// Try to load config and compute hash for staleness detection
	var currentState state.State
	var containerInfo *state.ContainerInfo

	cfg, _, err := config.Load(workspacePath, configPath)
	if err == nil {
		// Config exists, check for staleness
		configHash, hashErr := config.ComputeHash(cfg)
		if hashErr == nil {
			currentState, containerInfo, err = stateMgr.GetStateWithProjectAndHash(ctx, projectName, envKey, configHash)
		} else {
			currentState, containerInfo, err = stateMgr.GetStateWithProject(ctx, projectName, envKey)
		}
	} else {
		// No config or error loading it, just get basic state
		currentState, containerInfo, err = stateMgr.GetStateWithProject(ctx, projectName, envKey)
	}

	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	// Display status
	fmt.Fprintf(os.Stdout, "Workspace:  %s\n", workspacePath)
	if projectName != "" {
		fmt.Fprintf(os.Stdout, "Project:    %s\n", projectName)
	}
	fmt.Fprintf(os.Stdout, "Env Key:    %s\n", envKey)
	fmt.Fprintf(os.Stdout, "State:      %s\n", currentState)

	// Show SSH status
	if containerInfo != nil && ssh.HasSSHConfig(containerInfo.Name) {
		sshHost := envKey
		if projectName != "" {
			sshHost = projectName
		}
		fmt.Fprintf(os.Stdout, "SSH:        ssh %s.dcx\n", sshHost)
	} else if currentState != state.StateAbsent {
		fmt.Fprintf(os.Stdout, "SSH:        not configured (use 'dcx up --ssh' to enable)\n")
	}

	// Show shortcuts count
	if dcxCfg != nil && len(dcxCfg.Shortcuts) > 0 {
		fmt.Fprintf(os.Stdout, "Shortcuts:  %d defined (use 'dcx run --list' to view)\n", len(dcxCfg.Shortcuts))
	}

	if containerInfo != nil {
		fmt.Fprintf(os.Stdout, "\nPrimary Container:\n")
		fmt.Fprintf(os.Stdout, "  ID:       %s\n", containerInfo.ID[:12])
		fmt.Fprintf(os.Stdout, "  Name:     %s\n", containerInfo.Name)
		fmt.Fprintf(os.Stdout, "  Status:   %s\n", containerInfo.Status)
		if containerInfo.ConfigHash != "" {
			fmt.Fprintf(os.Stdout, "  Config:   %s\n", containerInfo.ConfigHash[:12])
		}
	}

	return nil
}
