package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/state"
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

	if result.State == state.StateAbsent {
		return fmt.Errorf("no devcontainer found, use 'dcx up' to create one")
	}

	containerInfo := result.ContainerInfo

	// Check shutdownAction setting if not forcing
	if !restartForce {
		cfg, _, loadErr := devcontainer.Load(cliCtx.WorkspacePath(), cliCtx.ConfigPath())
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

	// Stop the container(s), then bring the environment back up through the
	// service so the full start sequence runs (agent relaunch, secrets,
	// postStart hooks, SSH config) — a bare `docker start` leaves exec broken.
	if containerInfo.Running {
		spinner := ui.StartSpinner("Stopping devcontainer...")
		var stopErr error
		if containerInfo.IsSingleContainer() {
			stopErr = cliCtx.Docker.StopContainer(cliCtx.Ctx, containerInfo.ID, nil)
		} else {
			actualProject := containerInfo.GetComposeProject(cliCtx.Identifiers.ProjectName)
			configDir := containerInfo.GetConfigDir(cliCtx.WorkspacePath())
			stopErr = container.NewUnifiedRuntimeForExistingCompose(configDir, actualProject).Stop(cliCtx.Ctx)
		}
		if stopErr != nil {
			spinner.Fail("Failed to stop devcontainer")
			return fmt.Errorf("failed to stop devcontainer: %w", stopErr)
		}
		spinner.Success("Devcontainer stopped")
	}

	if err := cliCtx.Service.Up(cliCtx.Ctx, service.UpOptions{}); err != nil {
		return err
	}
	ui.Success("Devcontainer restarted")
	return nil
}
