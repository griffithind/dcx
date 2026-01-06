package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/compose"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop running containers",
	Long: `Stop devcontainer containers without removing them.

This is an offline-safe command that only stops running containers.
The containers and their data are preserved and can be started again
with 'dcx start'.`,
	RunE: runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
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
		fmt.Println("No environment found")
		return nil

	case state.StateCreated:
		fmt.Println("Environment is already stopped")
		return nil

	case state.StateRunning, state.StateStale, state.StateBroken:
		// Determine plan type from container labels
		if containerInfo != nil && containerInfo.Plan == docker.PlanSingle {
			// Single container - use Docker API directly
			if err := dockerClient.StopContainer(ctx, containerInfo.ID, nil); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		} else {
			// Compose plan - use docker compose
			runner := compose.NewRunnerFromEnvKey(workspacePath, envKey)
			if err := runner.Stop(ctx, compose.StopOptions{Verbose: verbose}); err != nil {
				return fmt.Errorf("failed to stop containers: %w", err)
			}
		}
		fmt.Println("Environment stopped")
		return nil

	default:
		return fmt.Errorf("unexpected state: %s", currentState)
	}
}
