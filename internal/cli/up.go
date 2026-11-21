package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	recreate bool
	rebuild  bool
	pull     bool
	hostsArg string
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
	upCmd.Flags().BoolVar(&pull, "pull", false, "re-pull the base image and re-fetch features (for moving tags like :latest); applies when the container is created, so add --recreate for an existing one")
	upCmd.Flags().StringVar(&hostsArg, "hosts", "", "widen SSH access beyond loopback when creating the container (e.g. --hosts=10.0.0.0/24, --hosts=any, --hosts=loopback); kept until changed with --recreate")
}

func runUp(cmd *cobra.Command, args []string) error {
	// --hosts is recorded on the container; without the flag Up keeps
	// what the container has.
	var access *container.SSHAccess
	if cmd.Flags().Changed("hosts") {
		a, err := parseHostsSpec(hostsArg)
		if err != nil {
			return fmt.Errorf("invalid --hosts: %w", err)
		}
		access = &a
	}

	cliCtx := NewCLIContext()
	defer cliCtx.Close()

	// Service.Up decides what to do (nothing, resume an unfinished setup,
	// start, create or recreate) and owns the complete sequence.
	// Full up sequence required
	changed, err := cliCtx.Service.Up(cliCtx.Ctx, service.UpOptions{
		Recreate:  recreate,
		Rebuild:   rebuild,
		Pull:      pull,
		SSHAccess: access,
	})
	if err != nil {
		return err
	}
	// With nothing to do, "Devcontainer is already running" says it all.
	if changed {
		ui.Success("Devcontainer started successfully")
	}
	return nil
}
