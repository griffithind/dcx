package cli

import (
	"github.com/griffithind/dcx/internal/service"
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
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	skipped, err := cliCtx.Service.Restart(cliCtx.Ctx, service.RestartOptions{
		StopOptions: service.StopOptions{Force: restartForce},
		Rebuild:     restartRebuild,
	})
	if err != nil {
		return err
	}
	if skipped {
		ui.Println("Skipping restart: shutdownAction is set to 'none'")
		ui.Println("Use --force to restart anyway")
		return nil
	}
	ui.Success("Devcontainer restarted")
	return nil
}
