package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ssh"
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

This command will:
1. Parse the devcontainer.json configuration
2. Build images if needed (or if --rebuild is specified)
3. Start containers using docker compose
4. Run lifecycle hooks (onCreate, postCreate, postStart)

This command may require network access for pulling images or features.`,
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
	out := output.Global()

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

	// Create environment service
	svc := service.NewEnvironmentService(dockerClient, workspacePath, configPath, verbose)

	// Start spinner for progress feedback
	spinner := output.NewSpinner("Starting devcontainer...")
	if !out.IsQuiet() && !out.IsJSON() {
		spinner.Start()
	}

	// Execute up
	upErr := svc.Up(ctx, service.UpOptions{
		Recreate:        recreate,
		Rebuild:         rebuild,
		Pull:            pull,
		SSHAgentEnabled: sshAgentEnabled,
		EnableSSH:       effectiveSSH,
	})

	// Stop spinner with appropriate message
	if !out.IsQuiet() && !out.IsJSON() {
		if upErr != nil {
			spinner.StopWithError("Failed to start devcontainer")
		} else {
			spinner.StopWithSuccess("Devcontainer started successfully")
		}
	}

	return upErr
}
