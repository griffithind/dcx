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
	recreate  bool
	rebuild   bool
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

	// Parse devcontainer configuration
	cfg, cfgPath, err := config.Load(workspacePath, configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if verbose {
		fmt.Printf("Loaded configuration from: %s\n", cfgPath)
	}

	// Load dcx.json configuration (optional)
	dcxCfg, err := config.LoadDcxConfig(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to load dcx.json: %w", err)
	}

	// Get project name from dcx.json
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = state.SanitizeProjectName(dcxCfg.Name)
		if verbose {
			fmt.Printf("Project name: %s\n", projectName)
		}
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

	// Check current state with hash check for staleness (check both project name and env key)
	currentState, _, err := stateMgr.GetStateWithProjectAndHash(ctx, projectName, envKey, configHash)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if verbose {
		fmt.Printf("Config hash: %s\n", configHash[:12])
		fmt.Printf("Current state: %s\n", currentState)
	}

	// Determine if SSH agent should be enabled
	sshAgentEnabled := !effectiveNoAgent && ssh.IsAgentAvailable()

	// Handle state transitions
	var isNewEnvironment bool
	var needsRebuild bool // Track if we need to rebuild due to stale state
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
		if err := runDownWithOptions(ctx, dockerClient, projectName, envKey, true, false); err != nil {
			return fmt.Errorf("failed to remove existing environment: %w", err)
		}
		// When recovering from stale state, always rebuild to ensure fresh images
		needsRebuild = true
		fallthrough
	case state.StateAbsent:
		// Create new environment with rebuild if state was stale or --rebuild flag was passed
		if err := createEnvironment(ctx, dockerClient, cfg, cfgPath, projectName, envKey, configHash, rebuild || needsRebuild); err != nil {
			return err
		}
		isNewEnvironment = true
	case state.StateCreated:
		// Just start the existing containers
		if err := startEnvironment(ctx, dockerClient, projectName, envKey); err != nil {
			return err
		}
	}

	// Pre-deploy agent binary before lifecycle hooks if SSH agent is enabled
	if sshAgentEnabled {
		stateMgr := state.NewManager(dockerClient)
		_, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
		if err == nil && containerInfo != nil {
			fmt.Println("Installing dcx agent...")
			if err := ssh.PreDeployAgent(ctx, containerInfo.Name); err != nil {
				return fmt.Errorf("failed to install dcx agent: %w", err)
			}
		}
	}

	// Run lifecycle hooks
	if err := runLifecycleHooks(ctx, dockerClient, cfg, projectName, envKey, isNewEnvironment, sshAgentEnabled); err != nil {
		return fmt.Errorf("lifecycle hooks failed: %w", err)
	}

	// Setup SSH server access if requested
	if effectiveSSH {
		if err := setupSSHAccess(ctx, dockerClient, cfg, projectName, envKey); err != nil {
			fmt.Printf("Warning: Failed to setup SSH access: %v\n", err)
		}
	}

	fmt.Println("Environment is ready")
	return nil
}

func setupSSHAccess(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, projectName, envKey string) error {
	// Get the primary container
	stateMgr := state.NewManager(dockerClient)
	_, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get container state: %w", err)
	}
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Deploy dcx binary to container
	binaryPath := ssh.GetContainerBinaryPath()
	if err := ssh.DeployToContainer(ctx, containerInfo.Name, binaryPath); err != nil {
		return fmt.Errorf("failed to deploy SSH server: %w", err)
	}

	// Determine user
	user := "root"
	if cfg != nil {
		if cfg.RemoteUser != "" {
			user = cfg.RemoteUser
		} else if cfg.ContainerUser != "" {
			user = cfg.ContainerUser
		}
		user = config.Substitute(user, &config.SubstitutionContext{
			LocalWorkspaceFolder: workspacePath,
		})
	}

	// Use project name as SSH host if available, otherwise env key
	// Always add .dcx suffix for clarity
	hostName := envKey
	if projectName != "" {
		hostName = projectName
	}
	hostName = hostName + ".dcx"
	if err := ssh.AddSSHConfig(hostName, containerInfo.Name, user); err != nil {
		return fmt.Errorf("failed to update SSH config: %w", err)
	}

	fmt.Printf("SSH configured: ssh %s\n", hostName)
	return nil
}

func createEnvironment(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, cfgPath, projectName, envKey, configHash string, forceRebuild bool) error {
	// Determine plan type
	if cfg.IsComposePlan() {
		return createComposeEnvironment(ctx, dockerClient, cfg, cfgPath, projectName, envKey, configHash, forceRebuild)
	}
	if cfg.IsSinglePlan() {
		return createSingleEnvironment(ctx, dockerClient, cfg, cfgPath, projectName, envKey, configHash, forceRebuild)
	}
	return fmt.Errorf("invalid configuration: no build plan detected")
}

func createComposeEnvironment(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, cfgPath, projectName, envKey, configHash string, forceRebuild bool) error {
	fmt.Println("Creating compose-based environment...")

	// Create compose runner
	runner, err := compose.NewRunner(workspacePath, cfgPath, cfg, projectName, envKey, configHash)
	if err != nil {
		return fmt.Errorf("failed to create compose runner: %w", err)
	}

	// Generate override file and run compose up
	// Rebuild is triggered by --rebuild flag OR when recovering from stale state
	if err := runner.Up(ctx, compose.UpOptions{
		Build:   rebuild,
		Rebuild: forceRebuild,
	}); err != nil {
		return fmt.Errorf("failed to start compose environment: %w", err)
	}

	return nil
}

func createSingleEnvironment(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, cfgPath, projectName, envKey, configHash string, forceRebuild bool) error {
	fmt.Println("Creating single-container environment...")

	// Create single-container runner
	runner := single.NewRunner(dockerClient, workspacePath, cfgPath, cfg, projectName, envKey, configHash)

	// Start the environment
	// For single containers, rebuild means rebuild the image
	if err := runner.Up(ctx, single.UpOptions{
		Build: rebuild || forceRebuild,
	}); err != nil {
		return fmt.Errorf("failed to start single-container environment: %w", err)
	}

	return nil
}

func startEnvironment(ctx context.Context, dockerClient *docker.Client, projectName, envKey string) error {
	fmt.Println("Starting existing containers...")

	runner := compose.NewRunnerFromEnvKey(workspacePath, projectName, envKey)
	if err := runner.Start(ctx); err != nil {
		return fmt.Errorf("failed to start containers: %w", err)
	}

	return nil
}

func runDownWithOptions(ctx context.Context, dockerClient *docker.Client, projectName, envKey string, removeVolumes, removeOrphans bool) error {
	// First check what type of environment we have
	stateMgr := state.NewManager(dockerClient)
	_, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
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
	runner := compose.NewRunnerFromEnvKey(workspacePath, projectName, envKey)
	return runner.Down(ctx, compose.DownOptions{
		RemoveVolumes: removeVolumes,
		RemoveOrphans: removeOrphans,
	})
}

func runLifecycleHooks(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, projectName, envKey string, isNew bool, sshAgentEnabled bool) error {
	// Get the primary container ID
	stateMgr := state.NewManager(dockerClient)
	_, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get container state: %w", err)
	}
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Create hook runner (agent binary is pre-deployed, so skip deployment in hooks)
	runner := lifecycle.NewHookRunner(dockerClient, containerInfo.ID, workspacePath, cfg, envKey, sshAgentEnabled, sshAgentEnabled)

	// Run appropriate hooks based on whether this is a new environment
	if isNew {
		return runner.RunAllCreateHooks(ctx)
	}
	return runner.RunStartHooks(ctx)
}
