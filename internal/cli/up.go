package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/compose"
	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/workspace"
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

	// Get project name from dcx.json
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = docker.SanitizeProjectName(dcxCfg.Name)
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

	// Check if we can do a quick start (smart detection)
	// Skip smart detection if --rebuild or --recreate or --pull are specified
	if !rebuild && !recreate && !pull {
		stateMgr := state.NewManager(dockerClient)
		envKey := workspace.ComputeID(workspacePath)

		// Try to load config and compute hash for staleness detection
		cfg, _, cfgErr := config.Load(workspacePath, configPath)
		if cfgErr == nil {
			// Use simple hash of raw JSON to match workspace builder
			var configHash string
			if raw := cfg.GetRawJSON(); len(raw) > 0 {
				configHash = config.ComputeSimpleHash(raw)
			}
			if configHash != "" {
				currentState, containerInfo, stateErr := stateMgr.GetStateWithProjectAndHash(ctx, projectName, envKey, configHash)
				if stateErr == nil {
					switch currentState {
					case state.StateRunning:
						// Already running, nothing to do
						if !out.IsQuiet() {
							out.Println(output.FormatSuccess("Environment is already running"))
						}
						return nil

					case state.StateCreated:
						// Containers exist but stopped, just start them (offline-safe)
						if !out.IsQuiet() && !out.IsJSON() {
							out.Printf("Environment exists and is up to date, starting...")
						}

						if err := quickStart(ctx, dockerClient, containerInfo, projectName, envKey); err != nil {
							return err
						}

						if !out.IsQuiet() {
							out.Println(output.FormatSuccess("Environment started"))
						}
						return nil

					// For ABSENT, STALE, BROKEN - continue to full up
					}
				}
			}
		}
	}

	// Full up sequence required
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

// quickStart starts existing containers without going through the full up sequence.
// This is an offline-safe operation.
func quickStart(ctx context.Context, dockerClient *docker.Client, containerInfo *state.ContainerInfo, projectName, envKey string) error {
	// Determine plan type (single-container vs compose)
	isSingleContainer := containerInfo != nil && (containerInfo.Plan == labels.BuildMethodImage ||
		containerInfo.Plan == labels.BuildMethodDockerfile)
	if isSingleContainer {
		// Single container - use Docker API directly
		if err := dockerClient.StartContainer(ctx, containerInfo.ID); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
	} else {
		// Compose plan - use docker compose
		actualProject := ""
		if containerInfo != nil {
			actualProject = containerInfo.ComposeProject
		}
		if actualProject == "" {
			actualProject = projectName
		}
		runner := compose.NewRunnerFromEnvKey(workspacePath, actualProject, envKey)
		if err := runner.Start(ctx); err != nil {
			return fmt.Errorf("failed to start containers: %w", err)
		}
	}
	return nil
}
