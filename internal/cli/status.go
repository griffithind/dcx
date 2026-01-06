package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
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
			currentState, containerInfo, err = stateMgr.GetStateWithHashCheck(ctx, envKey, configHash)
		} else {
			currentState, containerInfo, err = stateMgr.GetState(ctx, envKey)
		}
	} else {
		// No config or error loading it, just get basic state
		currentState, containerInfo, err = stateMgr.GetState(ctx, envKey)
	}

	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	// Display status
	fmt.Fprintf(os.Stdout, "Workspace:  %s\n", workspacePath)
	fmt.Fprintf(os.Stdout, "Env Key:    %s\n", envKey)
	fmt.Fprintf(os.Stdout, "State:      %s\n", currentState)

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
