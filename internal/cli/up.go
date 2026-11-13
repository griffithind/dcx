package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/state"
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
	upCmd.Flags().BoolVar(&pull, "pull", false, "force re-fetch remote features (useful when feature tags like :latest are updated)")
	upCmd.Flags().StringVar(&hostsArg, "hosts", "", "widen SSH access beyond loopback (e.g. --hosts=10.0.0.0/24 or --hosts=any)")
}

func runUp(cmd *cobra.Command, args []string) error {
	hosts, err := parseHostsSpec(hostsArg)
	if err != nil {
		return fmt.Errorf("invalid --hosts: %w", err)
	}

	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	// Fast path: nothing to do when the environment is already running and
	// up to date. Every other case (including starting a stopped container)
	// goes through Service.Up, which owns the complete start sequence: agent
	// relaunch, secret re-mount, postStart hooks and SSH config.
	if !rebuild && !recreate && !pull {
		plan, err := cliCtx.Service.Plan(cliCtx.Ctx, service.PlanOptions{})
		if err == nil && plan.Action == state.PlanActionNone {
			ui.Success("Devcontainer is already running")
			return nil
		}
	}

	// Full up sequence required
	if err := cliCtx.Service.Up(cliCtx.Ctx, service.UpOptions{
		Recreate:        recreate,
		Rebuild:         rebuild,
		Pull:            pull,
		SSHBindHost:     hosts.BindHost,
		SSHAllowedCIDRs: hosts.CIDRs,
	}); err != nil {
		return err
	}

	ui.Success("Devcontainer started successfully")
	return nil
}
