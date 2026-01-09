package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/runner"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var stopForce bool

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop running containers",
	Long: `Stop devcontainer containers without removing them.

This is an offline-safe command that only stops running containers.
The containers and their data are preserved and can be started again
with 'dcx start'.

If the devcontainer.json has shutdownAction set to "none", the container
will not be stopped unless --force is used.`,
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

	// Create service and get identifiers
	svc := service.NewEnvironmentService(dockerClient, workspacePath, configPath, verbose)
	ids, err := svc.GetIdentifiers()
	if err != nil {
		return fmt.Errorf("failed to get identifiers: %w", err)
	}

	// Check current state
	currentState, containerInfo, err := svc.GetStateMgr().GetStateWithProject(ctx, ids.ProjectName, ids.EnvKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	switch currentState {
	case state.StateAbsent:
		ui.Println("No devcontainer found")
		return nil

	case state.StateCreated:
		ui.Println("Devcontainer is already stopped")
		return nil

	case state.StateRunning, state.StateStale, state.StateBroken:
		// Check shutdownAction setting if not forcing
		if !stopForce {
			cfg, _, loadErr := config.Load(workspacePath, configPath)
			if loadErr == nil && cfg.ShutdownAction == "none" {
				ui.Println("Skipping stop: shutdownAction is set to 'none'")
				ui.Println("Use --force to stop anyway")
				return nil
			}
		}

		// Determine plan type from container labels (single-container vs compose)
		isSingleContainer := containerInfo != nil && (containerInfo.Plan == labels.BuildMethodImage ||
			containerInfo.Plan == labels.BuildMethodDockerfile)
		if isSingleContainer {
			// Single container - use Docker API directly
			if err := dockerClient.StopContainer(ctx, containerInfo.ID, nil); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		} else {
			// Compose plan - use docker compose
			actualProject := containerInfo.ComposeProject
			if actualProject == "" {
				actualProject = ids.ProjectName
			}
			r := runner.NewUnifiedRunnerForExisting(workspacePath, actualProject, ids.EnvKey)
			if err := r.Stop(ctx); err != nil {
				return fmt.Errorf("failed to stop containers: %w", err)
			}
		}
		ui.Success("Devcontainer stopped")
		return nil

	default:
		return fmt.Errorf("unexpected state: %s", currentState)
	}
}

func init() {
	stopCmd.Flags().BoolVarP(&stopForce, "force", "f", false, "force stop even if shutdownAction is 'none'")
}
