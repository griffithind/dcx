package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/compose"
	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/runner"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
)

var (
	removeVolumes bool
	removeOrphans bool
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop and remove containers",
	Long: `Stop and remove devcontainer containers.

This is an offline-safe command that stops and removes containers
managed by dcx. Optionally removes volumes and orphan containers.`,
	RunE: runDown,
}

func init() {
	downCmd.Flags().BoolVar(&removeVolumes, "volumes", false, "remove named volumes")
	downCmd.Flags().BoolVar(&removeOrphans, "remove-orphans", false, "remove containers not defined in compose file")
}

func runDown(cmd *cobra.Command, args []string) error {
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

	if currentState == state.StateAbsent {
		fmt.Println("No environment found")
		return nil
	}

	// Determine plan type from container labels
	if containerInfo != nil && containerInfo.Plan == docker.PlanSingle {
		// Single container - use Docker API directly
		// Stop if running
		if containerInfo.Running {
			if err := dockerClient.StopContainer(ctx, containerInfo.ID, nil); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}
		// Remove container (and optionally volumes)
		if err := dockerClient.RemoveContainer(ctx, containerInfo.ID, true, removeVolumes); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
	} else {
		// Compose plan - use docker compose
		// Use the actual compose project from container labels for migration support
		actualProject := containerInfo.ComposeProject
		if actualProject == "" {
			actualProject = projectName
		}
		composeRunner := compose.NewRunnerFromEnvKey(workspacePath, actualProject, envKey)
		if err := composeRunner.Down(ctx, runner.DownOptions{
			RemoveVolumes: removeVolumes,
			RemoveOrphans: removeOrphans,
		}); err != nil {
			return fmt.Errorf("failed to remove environment: %w", err)
		}
	}

	// Clean up SSH config entry using container name (used in marker)
	if containerInfo != nil {
		ssh.RemoveSSHConfig(containerInfo.Name)
	}

	fmt.Println("Environment removed")
	return nil
}
