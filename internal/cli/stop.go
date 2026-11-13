package cli

import (
	"github.com/griffithind/dcx/internal/service"
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
with 'dcx up'.

If the devcontainer.json has shutdownAction set to "none", the container
will not be stopped unless --force is used.`,
	RunE: runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	outcome, err := cliCtx.Service.Stop(cliCtx.Ctx, service.StopOptions{Force: stopForce})
	if err != nil {
		return err
	}
	switch outcome {
	case service.StopNotFound:
		ui.Println("No devcontainer found")
	case service.StopAlreadyStopped:
		ui.Println("Devcontainer is already stopped")
	case service.StopSkipped:
		ui.Println("Skipping stop: shutdownAction is set to 'none'")
		ui.Println("Use --force to stop anyway")
	case service.StopDone:
		ui.Success("Devcontainer stopped")
	}
	return nil
}

func init() {
	stopCmd.Flags().BoolVarP(&stopForce, "force", "f", false, "force stop even if shutdownAction is 'none'")
}
