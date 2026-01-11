package cli

import (
	"github.com/griffithind/dcx/internal/service"
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
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	return cliCtx.Service.DownWithIDs(cliCtx.Ctx, cliCtx.Identifiers.ProjectName, cliCtx.Identifiers.WorkspaceID, service.DownOptions{
		RemoveVolumes: removeVolumes,
		RemoveOrphans: removeOrphans,
	})
}
