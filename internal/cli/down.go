package cli

import (
	"github.com/griffithind/dcx/internal/service"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/ui"
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

	if err := cliCtx.Service.DownWithIDs(cliCtx.Ctx, cliCtx.Identifiers.ProjectName, cliCtx.Identifiers.WorkspaceID, service.DownOptions{
		RemoveVolumes: removeVolumes,
		RemoveOrphans: removeOrphans,
	}); err != nil {
		return err
	}
	// A user-requested down retires the workspace's SSH host key too (a
	// recreate inside `dcx up` keeps it, so editors' pins stay valid).
	if err := dcxssh.RemoveHostKey(cliCtx.Identifiers.WorkspaceID); err != nil {
		ui.Warning("Failed to remove SSH host key: %v", err)
	}
	return nil
}
