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

var (
	restartForce   bool
	restartRebuild bool
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the devcontainer",
	Long: `Stop and start devcontainer containers without rebuilding.

This command stops running containers and starts them again. It's useful
for applying configuration changes that don't require a full rebuild.

If the devcontainer.json has shutdownAction set to "none", the container
will not be restarted unless --force is used.

Use --rebuild to perform a full rebuild instead of just restart.`,
	RunE: runRestart,
}

func init() {
	restartCmd.Flags().BoolVarP(&restartForce, "force", "f", false, "force restart even if shutdownAction is 'none'")
	restartCmd.Flags().BoolVar(&restartRebuild, "rebuild", false, "perform full rebuild instead of restart")
	restartCmd.GroupID = "lifecycle"
	rootCmd.AddCommand(restartCmd)
}

func runRestart(cmd *cobra.Command, args []string) error {
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

	// Check current state
	currentState, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if currentState == state.StateAbsent {
		return fmt.Errorf("no environment found, use 'dcx up' to create one")
	}

	// Check shutdownAction setting if not forcing
	if !restartForce {
		cfg, _, loadErr := config.Load(workspacePath, configPath)
		if loadErr == nil && cfg.ShutdownAction == "none" {
			if !out.IsQuiet() {
				out.Println("Skipping restart: shutdownAction is set to 'none'")
				out.Println("Use --force to restart anyway")
			}
			return nil
		}
	}

	// If rebuild requested, use the up command logic
	if restartRebuild {
		// Set flags and call runUp
		recreate = true
		rebuild = true
		return runUp(cmd, args)
	}

	// Start spinner
	spinner := output.NewSpinner("Restarting devcontainer...")
	if !out.IsQuiet() && !out.IsJSON() {
		spinner.Start()
	}

	var restartErr error

	// Determine plan type from container labels (single-container vs compose)
	isSingleContainer := containerInfo != nil && (containerInfo.Plan == labels.BuildMethodImage ||
		containerInfo.Plan == labels.BuildMethodDockerfile)
	if isSingleContainer {
		// Single container - use Docker API directly
		if containerInfo.Running {
			if err := dockerClient.StopContainer(ctx, containerInfo.ID, nil); err != nil {
				restartErr = fmt.Errorf("failed to stop container: %w", err)
			}
		}
		if restartErr == nil {
			if err := dockerClient.StartContainer(ctx, containerInfo.ID); err != nil {
				restartErr = fmt.Errorf("failed to start container: %w", err)
			}
		}
	} else {
		// Compose plan - use docker compose
		actualProject := ""
		if containerInfo != nil {
			actualProject = containerInfo.ComposeProject
		}
		if actualProject == "" {
			actualProject = projectName
		}
		r := runner.NewUnifiedRunnerForExisting(workspacePath, actualProject, envKey)
		if err := r.Restart(ctx); err != nil {
			restartErr = fmt.Errorf("failed to restart containers: %w", err)
		}
	}

	// Stop spinner with appropriate message
	if !out.IsQuiet() && !out.IsJSON() {
		if restartErr != nil {
			spinner.StopWithError("Failed to restart devcontainer")
		} else {
			spinner.StopWithSuccess("Devcontainer restarted")
		}
	}

	return restartErr
}
