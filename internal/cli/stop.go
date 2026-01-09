package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/runner"
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
	// Initialize CLI context
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	// Get current state (allow any state)
	result, err := CheckState(cliCtx)
	if err != nil {
		return err
	}

	currentState := result.State
	containerInfo := result.ContainerInfo

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
			cfg, _, loadErr := config.Load(cliCtx.WorkspacePath(), cliCtx.ConfigPath())
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
			if err := cliCtx.DockerClient.StopContainer(cliCtx.Ctx, containerInfo.ID, nil); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		} else {
			// Compose plan - use docker compose
			actualProject := containerInfo.ComposeProject
			if actualProject == "" {
				actualProject = cliCtx.Identifiers.ProjectName
			}
			r := runner.NewUnifiedRunnerForExisting(cliCtx.WorkspacePath(), actualProject, cliCtx.Identifiers.EnvKey)
			if err := r.Stop(cliCtx.Ctx); err != nil {
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
