package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/compose"
	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/lifecycle"
	"github.com/griffithind/dcx/internal/single"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
)

var (
	recreate bool
	rebuild  bool
	noAgent  bool
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
	upCmd.Flags().BoolVar(&noAgent, "no-agent", false, "disable SSH agent forwarding")
}

func runUp(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Parse devcontainer configuration
	cfg, cfgPath, err := config.Load(workspacePath, configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if verbose {
		fmt.Printf("Loaded configuration from: %s\n", cfgPath)
	}

	// Validate configuration
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Initialize state manager
	stateMgr := state.NewManager(dockerClient)
	envKey := state.ComputeEnvKey(workspacePath)

	// Compute config hash for staleness detection
	configHash, err := config.ComputeHash(cfg)
	if err != nil {
		return fmt.Errorf("failed to compute config hash: %w", err)
	}

	// Check current state with hash check for staleness
	currentState, _, err := stateMgr.GetStateWithHashCheck(ctx, envKey, configHash)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if verbose {
		fmt.Printf("Config hash: %s\n", configHash[:12])
		fmt.Printf("Current state: %s\n", currentState)
	}

	// Handle state transitions
	var isNewEnvironment bool
	switch currentState {
	case state.StateRunning:
		if !recreate && !rebuild {
			fmt.Println("Environment is already running")
			return nil
		}
		// Fall through to recreate
		fallthrough
	case state.StateStale, state.StateBroken:
		if verbose {
			fmt.Println("Removing existing environment...")
		}
		// Remove existing containers
		if err := runDownWithOptions(ctx, dockerClient, envKey, true, false); err != nil {
			return fmt.Errorf("failed to remove existing environment: %w", err)
		}
		fallthrough
	case state.StateAbsent:
		// Create new environment
		if err := createEnvironment(ctx, dockerClient, cfg, cfgPath, envKey, configHash); err != nil {
			return err
		}
		isNewEnvironment = true
	case state.StateCreated:
		// Just start the existing containers
		if err := startEnvironment(ctx, dockerClient, envKey); err != nil {
			return err
		}
	}

	// Run lifecycle hooks
	if err := runLifecycleHooks(ctx, dockerClient, cfg, envKey, isNewEnvironment); err != nil {
		return fmt.Errorf("lifecycle hooks failed: %w", err)
	}

	fmt.Println("Environment is ready")
	return nil
}

func createEnvironment(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, cfgPath, envKey, configHash string) error {
	// Determine plan type
	if cfg.IsComposePlan() {
		return createComposeEnvironment(ctx, dockerClient, cfg, cfgPath, envKey, configHash)
	}
	if cfg.IsSinglePlan() {
		return createSingleEnvironment(ctx, dockerClient, cfg, cfgPath, envKey, configHash)
	}
	return fmt.Errorf("invalid configuration: no build plan detected")
}

func createComposeEnvironment(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, cfgPath, envKey, configHash string) error {
	fmt.Println("Creating compose-based environment...")

	// Create compose runner
	runner, err := compose.NewRunner(workspacePath, cfgPath, cfg, envKey, configHash)
	if err != nil {
		return fmt.Errorf("failed to create compose runner: %w", err)
	}

	// Generate override file and run compose up
	if err := runner.Up(ctx, compose.UpOptions{
		Build:   rebuild,
		Verbose: verbose,
	}); err != nil {
		return fmt.Errorf("failed to start compose environment: %w", err)
	}

	return nil
}

func createSingleEnvironment(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, cfgPath, envKey, configHash string) error {
	fmt.Println("Creating single-container environment...")

	// Create single-container runner
	runner := single.NewRunner(dockerClient, workspacePath, cfgPath, cfg, envKey, configHash)

	// Start the environment
	if err := runner.Up(ctx, single.UpOptions{
		Build:   rebuild,
		Verbose: verbose,
	}); err != nil {
		return fmt.Errorf("failed to start single-container environment: %w", err)
	}

	return nil
}

func startEnvironment(ctx context.Context, dockerClient *docker.Client, envKey string) error {
	fmt.Println("Starting existing containers...")

	runner := compose.NewRunnerFromEnvKey(workspacePath, envKey)
	if err := runner.Start(ctx, compose.StartOptions{Verbose: verbose}); err != nil {
		return fmt.Errorf("failed to start containers: %w", err)
	}

	return nil
}

func runDownWithOptions(ctx context.Context, dockerClient *docker.Client, envKey string, removeVolumes, removeOrphans bool) error {
	// First check what type of environment we have
	stateMgr := state.NewManager(dockerClient)
	_, containerInfo, err := stateMgr.GetState(ctx, envKey)
	if err != nil {
		return err
	}

	if containerInfo != nil && containerInfo.Plan == docker.PlanSingle {
		// Single container - use Docker API directly
		if containerInfo.Running {
			if err := dockerClient.StopContainer(ctx, containerInfo.ID, nil); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}
		if err := dockerClient.RemoveContainer(ctx, containerInfo.ID, true); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
		return nil
	}

	// Compose plan - use docker compose
	runner := compose.NewRunnerFromEnvKey(workspacePath, envKey)
	return runner.Down(ctx, compose.DownOptions{
		RemoveVolumes: removeVolumes,
		RemoveOrphans: removeOrphans,
		Verbose:       verbose,
	})
}

func runLifecycleHooks(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, envKey string, isNew bool) error {
	// Get the primary container ID
	stateMgr := state.NewManager(dockerClient)
	_, containerInfo, err := stateMgr.GetState(ctx, envKey)
	if err != nil {
		return fmt.Errorf("failed to get container state: %w", err)
	}
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Determine if SSH agent should be enabled for lifecycle hooks
	sshAgentEnabled := !noAgent && ssh.IsAgentAvailable()

	// Create hook runner
	runner := lifecycle.NewHookRunner(dockerClient, containerInfo.ID, workspacePath, cfg, verbose, envKey, sshAgentEnabled)

	// Run appropriate hooks based on whether this is a new environment
	if isNew {
		return runner.RunAllCreateHooks(ctx)
	}
	return runner.RunStartHooks(ctx)
}
