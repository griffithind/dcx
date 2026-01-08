package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/runner"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/workspace"
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
	out := output.Global()

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
		projectName = docker.SanitizeProjectName(dcxCfg.Name)
	}

	// Initialize state manager
	stateMgr := state.NewManager(dockerClient)
	envKey := workspace.ComputeID(workspacePath)

	// Check current state (check both project name and env key for migration)
	currentState, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	switch currentState {
	case state.StateAbsent:
		if !out.IsQuiet() {
			out.Println("No environment found")
		}
		return nil

	case state.StateCreated:
		if !out.IsQuiet() {
			out.Println("Environment is already stopped")
		}
		return nil

	case state.StateRunning, state.StateStale, state.StateBroken:
		// Check shutdownAction setting if not forcing
		if !stopForce {
			cfg, _, loadErr := config.Load(workspacePath, configPath)
			if loadErr == nil && cfg.ShutdownAction == "none" {
				if !out.IsQuiet() {
					out.Println("Skipping stop: shutdownAction is set to 'none'")
					out.Println("Use --force to stop anyway")
				}
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
			// Use the actual compose project from container labels for migration support
			actualProject := containerInfo.ComposeProject
			if actualProject == "" {
				actualProject = projectName
			}
			r := runner.NewUnifiedRunnerForExisting(workspacePath, actualProject, envKey)
			if err := r.Stop(ctx); err != nil {
				return fmt.Errorf("failed to stop containers: %w", err)
			}
		}
		if !out.IsQuiet() {
			out.Println(output.FormatSuccess("Environment stopped"))
		}
		return nil

	default:
		return fmt.Errorf("unexpected state: %s", currentState)
	}
}

func init() {
	stopCmd.Flags().BoolVarP(&stopForce, "force", "f", false, "force stop even if shutdownAction is 'none'")
}
