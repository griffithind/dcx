package cli

import (
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	recreate bool
	rebuild  bool
	pull     bool
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Start the devcontainer environment",
	Long: `Start the devcontainer environment, building if necessary.

This command is smart about what it needs to do:
- If containers exist and are up to date, just starts them (offline-safe)
- If containers are stale or missing, performs full build/create sequence
- Use --rebuild to force image rebuild
- Use --recreate to force container recreation

Lifecycle hooks run as appropriate based on the action taken.`,
	RunE: runUp,
}

func init() {
	upCmd.Flags().BoolVar(&recreate, "recreate", false, "force recreate containers")
	upCmd.Flags().BoolVar(&rebuild, "rebuild", false, "force rebuild images")
	upCmd.Flags().BoolVar(&pull, "pull", false, "force re-fetch remote features (useful when feature tags like :latest are updated)")
}

func runUp(cmd *cobra.Command, args []string) error {
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	// Check if we can do a quick start (smart detection)
	// Skip smart detection if --rebuild or --recreate or --pull are specified
	if !rebuild && !recreate && !pull {
		plan, err := cliCtx.Service.Plan(cliCtx.Ctx, service.PlanOptions{})
		if err == nil {
			switch plan.Action {
			case state.PlanActionNone:
				// Already running, nothing to do
				ui.Success("Devcontainer is already running")
				return nil

			case state.PlanActionStart:
				// Containers exist but stopped, just start them (offline-safe)
				ui.Printf("Devcontainer exists and is up to date, starting...")
				if err := cliCtx.Service.QuickStart(cliCtx.Ctx, plan.ContainerInfo, cliCtx.Identifiers.ProjectName, cliCtx.Identifiers.WorkspaceID); err != nil {
					return err
				}
				ui.Success("Devcontainer started")
				return nil

				// For CREATE, RECREATE, REBUILD - continue to full up
			}
		}
	}

	// Full up sequence required
	if err := cliCtx.Service.Up(cliCtx.Ctx, service.UpOptions{
		Recreate: recreate,
		Rebuild:  rebuild,
		Pull:     pull,
	}); err != nil {
		return err
	}

	ui.Success("Devcontainer started successfully")
	return nil
}
