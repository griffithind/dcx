package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/runner"
	"github.com/griffithind/dcx/internal/ui"
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
	// Initialize CLI context
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	// Get current state
	result, err := CheckState(cliCtx)
	if err != nil {
		return err
	}

	if result.State == container.StateAbsent {
		return fmt.Errorf("no devcontainer found, use 'dcx up' to create one")
	}

	containerInfo := result.ContainerInfo

	// Check shutdownAction setting if not forcing
	if !restartForce {
		cfg, _, loadErr := config.Load(cliCtx.WorkspacePath(), cliCtx.ConfigPath())
		if loadErr == nil && cfg.ShutdownAction == "none" {
			ui.Println("Skipping restart: shutdownAction is set to 'none'")
			ui.Println("Use --force to restart anyway")
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
	spinner := ui.StartSpinner("Restarting devcontainer...")

	var restartErr error

	// Determine plan type from container labels (single-container vs compose)
	isSingleContainer := containerInfo != nil && (containerInfo.Plan == labels.BuildMethodImage ||
		containerInfo.Plan == labels.BuildMethodDockerfile)
	if isSingleContainer {
		// Single container - use Docker API directly
		if containerInfo.Running {
			if err := cliCtx.DockerClient.StopContainer(cliCtx.Ctx, containerInfo.ID, nil); err != nil {
				restartErr = fmt.Errorf("failed to stop container: %w", err)
			}
		}
		if restartErr == nil {
			if err := cliCtx.DockerClient.StartContainer(cliCtx.Ctx, containerInfo.ID); err != nil {
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
			actualProject = cliCtx.Identifiers.ProjectName
		}
		r := runner.NewUnifiedRunnerForExisting(cliCtx.WorkspacePath(), actualProject, cliCtx.Identifiers.EnvKey)
		if err := r.Restart(cliCtx.Ctx); err != nil {
			restartErr = fmt.Errorf("failed to restart containers: %w", err)
		}
	}

	// Stop spinner with appropriate message
	if restartErr != nil {
		spinner.Fail("Failed to restart devcontainer")
	} else {
		spinner.Success("Devcontainer restarted")
	}

	return restartErr
}
