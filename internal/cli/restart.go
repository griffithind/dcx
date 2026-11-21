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
	Long: `Stop the devcontainer and bring it up again.

The start goes through 'dcx up', so the agent, runtime secrets and
postStartCommand are set up again. If devcontainer.json changed since the
container was created, it is recreated.

If devcontainer.json sets shutdownAction to "none", the container is not
restarted unless --force is given.

--rebuild rebuilds the images and recreates the container (shutdownAction
does not apply to it).`,
	RunE: runRestart,
}

func init() {
	restartCmd.Flags().BoolVarP(&restartForce, "force", "f", false, "force restart even if shutdownAction is 'none'")
	restartCmd.Flags().BoolVar(&restartRebuild, "rebuild", false, "perform full rebuild instead of restart")
	restartCmd.GroupID = "lifecycle"
	rootCmd.AddCommand(restartCmd)
}

func runRestart(cmd *cobra.Command, args []string) error {
	cliCtx := NewCLIContext()
	defer cliCtx.Close()

	skipped, err := cliCtx.Service.Restart(cliCtx.Ctx, service.RestartOptions{
		StopOptions: service.StopOptions{Force: restartForce},
		Rebuild:     restartRebuild,
	})
	if err != nil {
		return err
	}
	if skipped {
		ui.Info("Skipping restart: shutdownAction is set to 'none'")
		ui.Info("Use --force to restart anyway")
		return nil
	}
	ui.Success("Devcontainer restarted")
	return nil
}
