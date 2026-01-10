package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ssh/agent"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	recreate  bool
	rebuild   bool
	pull      bool
	noAgent   bool
	enableSSH bool
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
	upCmd.Flags().BoolVar(&noAgent, "no-agent", false, "disable SSH agent forwarding")
	upCmd.Flags().BoolVar(&enableSSH, "ssh", false, "enable SSH server access")
}

func runUp(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := container.NewDockerClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Load devcontainer.json for up options from customizations.dcx
	cfg, _, err := devcontainer.Load(workspacePath, configPath)
	if err != nil {
		return fmt.Errorf("failed to load devcontainer.json: %w", err)
	}

	// Get DCX customizations
	dcxCustom := devcontainer.GetDcxCustomizations(cfg)

	// Apply customizations.dcx up options (CLI flags take precedence)
	effectiveSSH := enableSSH
	effectiveNoAgent := noAgent
	if dcxCustom != nil {
		if !cmd.Flags().Changed("ssh") && dcxCustom.Up.SSH {
			effectiveSSH = true
		}
		if !cmd.Flags().Changed("no-agent") && dcxCustom.Up.NoAgent {
			effectiveNoAgent = true
		}
	}

	// Determine if SSH agent should be enabled
	sshAgentEnabled := !effectiveNoAgent && agent.IsAvailable()

	// Create service
	svc := service.NewDevContainerService(dockerClient, workspacePath, configPath, verbose)
	defer svc.Close()

	// Check if we can do a quick start (smart detection)
	// Skip smart detection if --rebuild or --recreate or --pull are specified
	if !rebuild && !recreate && !pull {
		plan, err := svc.Plan(ctx, service.PlanOptions{})
		if err == nil {
			switch plan.Action {
			case state.PlanActionNone:
				// Already running, nothing to do
				ui.Success("Devcontainer is already running")
				return nil

			case state.PlanActionStart:
				// Containers exist but stopped, just start them (offline-safe)
				ui.Printf("Devcontainer exists and is up to date, starting...")
				ids, _ := svc.GetIdentifiers()
				if err := svc.QuickStart(ctx, plan.ContainerInfo, ids.ProjectName, ids.WorkspaceID); err != nil {
					return err
				}
				ui.Success("Devcontainer started")
				return nil

				// For CREATE, RECREATE, REBUILD - continue to full up
			}
		}
	}

	// Full up sequence required
	if err := svc.Up(ctx, service.UpOptions{
		Recreate:        recreate,
		Rebuild:         rebuild,
		Pull:            pull,
		SSHAgentEnabled: sshAgentEnabled,
		EnableSSH:       effectiveSSH,
	}); err != nil {
		return err
	}

	ui.Success("Devcontainer started successfully")
	return nil
}
