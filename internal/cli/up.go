package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ssh"
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
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Load dcx.json configuration (optional) for up options
	dcxCfg, err := config.LoadDcxConfig(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to load dcx.json: %w", err)
	}

	// Apply dcx.json up options (CLI flags take precedence)
	effectiveSSH := enableSSH
	effectiveNoAgent := noAgent
	if dcxCfg != nil {
		if !cmd.Flags().Changed("ssh") && dcxCfg.Up.SSH {
			effectiveSSH = true
		}
		if !cmd.Flags().Changed("no-agent") && dcxCfg.Up.NoAgent {
			effectiveNoAgent = true
		}
	}

	// Determine if SSH agent should be enabled
	sshAgentEnabled := !effectiveNoAgent && ssh.IsAgentAvailable()

	// Create service
	svc := service.NewEnvironmentService(dockerClient, workspacePath, configPath, verbose)

	// Check if we can do a quick start (smart detection)
	// Skip smart detection if --rebuild or --recreate or --pull are specified
	if !rebuild && !recreate && !pull {
		plan, err := svc.Plan(ctx, service.PlanOptions{})
		if err == nil {
			switch plan.Action {
			case service.PlanActionNone:
				// Already running, nothing to do
				ui.Success("Devcontainer is already running")
				return nil

			case service.PlanActionStart:
				// Containers exist but stopped, just start them (offline-safe)
				ui.Printf("Devcontainer exists and is up to date, starting...")
				if err := svc.QuickStart(ctx, plan.ContainerInfo, plan.Info.ProjectName, plan.Info.EnvKey); err != nil {
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
