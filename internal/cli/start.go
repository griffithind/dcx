package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/compose"
	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start existing containers",
	Long: `Start existing devcontainer containers without rebuilding.

This is an offline-safe command that only starts containers that were
previously created. It will never pull images, rebuild, or recreate
containers. Use 'dcx up' if you need to create or rebuild the environment.

If the environment does not exist, this command will fail with an
instruction to run 'dcx up' while online.`,
	RunE: runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
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
	envKey := state.ComputeEnvKey(workspacePath)

	// Check current state (check both project name and env key for migration)
	currentState, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	switch currentState {
	case state.StateRunning:
		fmt.Println("Environment is already running")
		return nil

	case state.StateCreated:
		// Determine plan type from container labels
		if containerInfo != nil && containerInfo.Plan == docker.PlanSingle {
			// Single container - use Docker API directly
			if err := dockerClient.StartContainer(ctx, containerInfo.ID); err != nil {
				return fmt.Errorf("failed to start container: %w", err)
			}
		} else {
			// Compose plan - use docker compose
			// Use the actual compose project from container labels for migration support
			actualProject := containerInfo.ComposeProject
			if actualProject == "" {
				actualProject = projectName
			}
			runner := compose.NewRunnerFromEnvKey(workspacePath, actualProject, envKey)
			if err := runner.Start(ctx, compose.StartOptions{Verbose: verbose}); err != nil {
				return fmt.Errorf("failed to start containers: %w", err)
			}
		}
		fmt.Println("Environment started")
		return nil

	case state.StateAbsent:
		return fmt.Errorf("no environment found; run 'dcx up' while online to create one")

	case state.StateStale:
		return fmt.Errorf("environment is stale (config changed); run 'dcx up' while online to recreate")

	case state.StateBroken:
		return fmt.Errorf("environment is in broken state; run 'dcx up --recreate' while online to fix")

	default:
		return fmt.Errorf("unexpected state: %s", currentState)
	}
}
