// Package service provides high-level orchestration for devcontainer environments.
// It abstracts the differences between compose and single-container runners,
// and coordinates config loading, state management, and lifecycle hooks.
package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/griffithind/dcx/internal/compose"
	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lifecycle"
	runnerPkg "github.com/griffithind/dcx/internal/runner"
	"github.com/griffithind/dcx/internal/single"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
)

// EnvironmentService orchestrates devcontainer environment operations.
type EnvironmentService struct {
	dockerClient  *docker.Client
	stateMgr      *state.Manager
	workspacePath string
	configPath    string // optional override
	verbose       bool
}

// NewEnvironmentService creates a new environment service.
func NewEnvironmentService(dockerClient *docker.Client, workspacePath, configPath string, verbose bool) *EnvironmentService {
	return &EnvironmentService{
		dockerClient:  dockerClient,
		stateMgr:      state.NewManager(dockerClient),
		workspacePath: workspacePath,
		configPath:    configPath,
		verbose:       verbose,
	}
}

// EnvironmentInfo contains resolved environment configuration.
type EnvironmentInfo struct {
	Config      *config.DevcontainerConfig
	ConfigPath  string
	DcxConfig   *config.DcxConfig
	ProjectName string
	EnvKey      string
	ConfigHash  string
}

// LoadEnvironmentInfo loads and validates the environment configuration.
func (s *EnvironmentService) LoadEnvironmentInfo() (*EnvironmentInfo, error) {
	// Load devcontainer configuration
	cfg, cfgPath, err := config.Load(s.workspacePath, s.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	if s.verbose {
		fmt.Printf("Loaded configuration from: %s\n", cfgPath)
	}

	// Load dcx.json configuration (optional)
	dcxCfg, err := config.LoadDcxConfig(s.workspacePath)
	if err != nil {
		return nil, fmt.Errorf("failed to load dcx.json: %w", err)
	}

	// Get project name from dcx.json
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = state.SanitizeProjectName(dcxCfg.Name)
		if s.verbose {
			fmt.Printf("Project name: %s\n", projectName)
		}
	}

	// Validate configuration
	if err := config.Validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Compute identifiers
	envKey := state.ComputeEnvKey(s.workspacePath)
	configHash, err := config.ComputeHash(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to compute config hash: %w", err)
	}

	if s.verbose {
		fmt.Printf("Env key: %s\n", envKey)
		fmt.Printf("Config hash: %s\n", configHash[:12])
	}

	return &EnvironmentInfo{
		Config:      cfg,
		ConfigPath:  cfgPath,
		DcxConfig:   dcxCfg,
		ProjectName: projectName,
		EnvKey:      envKey,
		ConfigHash:  configHash,
	}, nil
}

// GetState returns the current state of the environment.
func (s *EnvironmentService) GetState(ctx context.Context, info *EnvironmentInfo) (state.State, *state.ContainerInfo, error) {
	return s.stateMgr.GetStateWithProjectAndHash(ctx, info.ProjectName, info.EnvKey, info.ConfigHash)
}

// GetStateBasic returns the current state without hash checking.
func (s *EnvironmentService) GetStateBasic(ctx context.Context, projectName, envKey string) (state.State, *state.ContainerInfo, error) {
	return s.stateMgr.GetStateWithProject(ctx, projectName, envKey)
}

// CreateRunner creates the appropriate runner based on configuration.
func (s *EnvironmentService) CreateRunner(info *EnvironmentInfo) (runnerPkg.Environment, error) {
	if info.Config.IsComposePlan() {
		r, err := compose.NewRunner(
			s.dockerClient,
			s.workspacePath,
			info.ConfigPath,
			info.Config,
			info.ProjectName,
			info.EnvKey,
			info.ConfigHash,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create compose runner: %w", err)
		}
		return r, nil
	}

	if info.Config.IsSinglePlan() {
		r := single.NewRunner(
			s.dockerClient,
			s.workspacePath,
			info.ConfigPath,
			info.Config,
			info.ProjectName,
			info.EnvKey,
			info.ConfigHash,
		)
		return r, nil
	}

	return nil, fmt.Errorf("invalid configuration: no build plan detected")
}

// UpOptions configures the Up operation.
type UpOptions struct {
	Recreate        bool
	Rebuild         bool
	Pull            bool
	SSHAgentEnabled bool
	EnableSSH       bool
}

// Up brings up the environment, building if necessary.
func (s *EnvironmentService) Up(ctx context.Context, opts UpOptions) error {
	info, err := s.LoadEnvironmentInfo()
	if err != nil {
		return err
	}

	// Check current state
	currentState, containerInfo, err := s.GetState(ctx, info)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if s.verbose {
		fmt.Printf("Current state: %s\n", currentState)
	}

	// Handle state transitions
	var isNewEnvironment bool
	var needsRebuild bool

	switch currentState {
	case state.StateRunning:
		if !opts.Recreate && !opts.Rebuild {
			fmt.Println("Environment is already running")
			return nil
		}
		fallthrough
	case state.StateStale, state.StateBroken:
		if s.verbose {
			fmt.Println("Removing existing environment...")
		}
		if err := s.Down(ctx, info, DownOptions{RemoveVolumes: true}); err != nil {
			return fmt.Errorf("failed to remove existing environment: %w", err)
		}
		needsRebuild = true
		fallthrough
	case state.StateAbsent:
		if err := s.create(ctx, info, opts.Rebuild || needsRebuild, opts.Pull); err != nil {
			return err
		}
		isNewEnvironment = true
	case state.StateCreated:
		if err := s.start(ctx, info); err != nil {
			return err
		}
	}

	// Pre-deploy agent binary before lifecycle hooks if SSH agent is enabled
	if opts.SSHAgentEnabled {
		_, containerInfo, err := s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.EnvKey)
		if err == nil && containerInfo != nil {
			fmt.Println("Installing dcx agent...")
			if err := ssh.PreDeployAgent(ctx, containerInfo.Name); err != nil {
				return fmt.Errorf("failed to install dcx agent: %w", err)
			}
		}
	}

	// Run lifecycle hooks
	if err := s.runLifecycleHooks(ctx, info, isNewEnvironment, opts.SSHAgentEnabled); err != nil {
		return fmt.Errorf("lifecycle hooks failed: %w", err)
	}

	// Setup SSH server access if requested
	if opts.EnableSSH {
		if err := s.setupSSHAccess(ctx, info, containerInfo); err != nil {
			fmt.Printf("Warning: Failed to setup SSH access: %v\n", err)
		}
	}

	fmt.Println("Environment is ready")
	return nil
}

// create creates a new environment.
func (s *EnvironmentService) create(ctx context.Context, info *EnvironmentInfo, forceRebuild, forcePull bool) error {
	envRunner, err := s.CreateRunner(info)
	if err != nil {
		return err
	}

	if info.Config.IsComposePlan() {
		fmt.Println("Creating compose-based environment...")
	} else {
		fmt.Println("Creating single-container environment...")
	}

	return envRunner.Up(ctx, runnerPkg.UpOptions{
		Build:   forceRebuild,
		Rebuild: forceRebuild,
		Pull:    forcePull,
	})
}

// start starts an existing stopped environment.
func (s *EnvironmentService) start(ctx context.Context, info *EnvironmentInfo) error {
	fmt.Println("Starting existing containers...")

	envRunner, err := s.CreateRunner(info)
	if err != nil {
		return err
	}

	return envRunner.Start(ctx)
}

// DownOptions configures the Down operation.
type DownOptions struct {
	RemoveVolumes bool
	RemoveOrphans bool
}

// Down removes the environment.
func (s *EnvironmentService) Down(ctx context.Context, info *EnvironmentInfo, opts DownOptions) error {
	currentState, containerInfo, err := s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.EnvKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if currentState == state.StateAbsent {
		fmt.Println("No environment found")
		return nil
	}

	// Handle based on plan type
	if containerInfo != nil && containerInfo.Plan == docker.PlanSingle {
		if containerInfo.Running {
			if err := s.dockerClient.StopContainer(ctx, containerInfo.ID, nil); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}
		if err := s.dockerClient.RemoveContainer(ctx, containerInfo.ID, true, opts.RemoveVolumes); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
	} else {
		actualProject := containerInfo.ComposeProject
		if actualProject == "" {
			actualProject = info.ProjectName
		}
		r := compose.NewRunnerFromEnvKey(s.workspacePath, actualProject, info.EnvKey)
		if err := r.Down(ctx, runnerPkg.DownOptions{
			RemoveVolumes: opts.RemoveVolumes,
			RemoveOrphans: opts.RemoveOrphans,
		}); err != nil {
			return fmt.Errorf("failed to remove environment: %w", err)
		}
	}

	// Clean up SSH config entry
	if containerInfo != nil {
		ssh.RemoveSSHConfig(containerInfo.Name)
	}

	return nil
}

// DownWithEnvKey removes the environment using just project name and env key.
func (s *EnvironmentService) DownWithEnvKey(ctx context.Context, projectName, envKey string, opts DownOptions) error {
	currentState, containerInfo, err := s.stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if currentState == state.StateAbsent {
		fmt.Println("No environment found")
		return nil
	}

	// Handle based on plan type
	if containerInfo != nil && containerInfo.Plan == docker.PlanSingle {
		if containerInfo.Running {
			if err := s.dockerClient.StopContainer(ctx, containerInfo.ID, nil); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}
		if err := s.dockerClient.RemoveContainer(ctx, containerInfo.ID, true, opts.RemoveVolumes); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
	} else {
		actualProject := ""
		if containerInfo != nil {
			actualProject = containerInfo.ComposeProject
		}
		if actualProject == "" {
			actualProject = projectName
		}
		r := compose.NewRunnerFromEnvKey(s.workspacePath, actualProject, envKey)
		if err := r.Down(ctx, runnerPkg.DownOptions{
			RemoveVolumes: opts.RemoveVolumes,
			RemoveOrphans: opts.RemoveOrphans,
		}); err != nil {
			return fmt.Errorf("failed to remove environment: %w", err)
		}
	}

	// Clean up SSH config entry
	if containerInfo != nil {
		ssh.RemoveSSHConfig(containerInfo.Name)
	}

	fmt.Println("Environment removed")
	return nil
}

// BuildOptions configures the Build operation.
type BuildOptions struct {
	NoCache bool
	Pull    bool
}

// Build builds the environment images without starting containers.
func (s *EnvironmentService) Build(ctx context.Context, opts BuildOptions) error {
	info, err := s.LoadEnvironmentInfo()
	if err != nil {
		return err
	}

	envRunner, err := s.CreateRunner(info)
	if err != nil {
		return err
	}

	if info.Config.IsComposePlan() {
		fmt.Println("Building compose-based environment...")
	}

	if err := envRunner.Build(ctx, runnerPkg.BuildOptions{
		NoCache: opts.NoCache,
		Pull:    opts.Pull,
	}); err != nil {
		return fmt.Errorf("failed to build: %w", err)
	}

	fmt.Println("Build complete")
	return nil
}

// StopOptions configures the Stop operation.
type StopOptions struct {
	Force bool // Force stop even if shutdownAction is "none"
}

// Stop stops the running environment.
// Respects the shutdownAction setting unless Force is true.
func (s *EnvironmentService) Stop(ctx context.Context, info *EnvironmentInfo, opts StopOptions) error {
	// Check shutdownAction setting
	if !opts.Force && info.Config.ShutdownAction == "none" {
		fmt.Println("Skipping stop: shutdownAction is set to 'none'")
		fmt.Println("Use --force to stop anyway")
		return nil
	}

	envRunner, err := s.CreateRunner(info)
	if err != nil {
		return err
	}

	return envRunner.Stop(ctx)
}

// runLifecycleHooks runs appropriate lifecycle hooks based on whether this is a new environment.
func (s *EnvironmentService) runLifecycleHooks(ctx context.Context, info *EnvironmentInfo, isNew bool, sshAgentEnabled bool) error {
	_, containerInfo, err := s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.EnvKey)
	if err != nil {
		return fmt.Errorf("failed to get container state: %w", err)
	}
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Create hook runner (agent binary is pre-deployed, so skip deployment in hooks)
	hookRunner := lifecycle.NewHookRunner(
		s.dockerClient,
		containerInfo.ID,
		s.workspacePath,
		info.Config,
		info.EnvKey,
		sshAgentEnabled,
		sshAgentEnabled, // skip deploy if already deployed
	)

	// Resolve features to get their lifecycle hooks
	if len(info.Config.Features) > 0 {
		configDir := filepath.Dir(info.ConfigPath)
		mgr, err := features.NewManager(configDir)
		if err == nil {
			resolvedFeatures, err := mgr.ResolveAll(ctx, info.Config.Features, info.Config.OverrideFeatureInstallOrder)
			if err == nil && len(resolvedFeatures) > 0 {
				var onCreateHooks, postCreateHooks, postStartHooks []lifecycle.FeatureHook

				for _, fh := range features.CollectOnCreateCommands(resolvedFeatures) {
					onCreateHooks = append(onCreateHooks, lifecycle.FeatureHook{
						FeatureID:   fh.FeatureID,
						FeatureName: fh.FeatureName,
						Command:     fh.Command,
					})
				}
				for _, fh := range features.CollectPostCreateCommands(resolvedFeatures) {
					postCreateHooks = append(postCreateHooks, lifecycle.FeatureHook{
						FeatureID:   fh.FeatureID,
						FeatureName: fh.FeatureName,
						Command:     fh.Command,
					})
				}
				for _, fh := range features.CollectPostStartCommands(resolvedFeatures) {
					postStartHooks = append(postStartHooks, lifecycle.FeatureHook{
						FeatureID:   fh.FeatureID,
						FeatureName: fh.FeatureName,
						Command:     fh.Command,
					})
				}

				hookRunner.SetFeatureHooks(onCreateHooks, postCreateHooks, postStartHooks)
			}
		}
	}

	// Run appropriate hooks based on whether this is a new environment
	if isNew {
		return hookRunner.RunAllCreateHooks(ctx)
	}
	return hookRunner.RunStartHooks(ctx)
}

// setupSSHAccess configures SSH access to the container.
func (s *EnvironmentService) setupSSHAccess(ctx context.Context, info *EnvironmentInfo, containerInfo *state.ContainerInfo) error {
	if containerInfo == nil {
		_, containerInfo, _ = s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.EnvKey)
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
	if info.Config != nil {
		if info.Config.RemoteUser != "" {
			user = info.Config.RemoteUser
		} else if info.Config.ContainerUser != "" {
			user = info.Config.ContainerUser
		}
		user = config.Substitute(user, &config.SubstitutionContext{
			LocalWorkspaceFolder: s.workspacePath,
		})
	}

	// Use project name as SSH host if available, otherwise env key
	hostName := info.EnvKey
	if info.ProjectName != "" {
		hostName = info.ProjectName
	}
	hostName = hostName + ".dcx"
	if err := ssh.AddSSHConfig(hostName, containerInfo.Name, user); err != nil {
		return fmt.Errorf("failed to update SSH config: %w", err)
	}

	fmt.Printf("SSH configured: ssh %s\n", hostName)
	return nil
}

// GetStateMgr returns the state manager for direct access when needed.
func (s *EnvironmentService) GetStateMgr() *state.Manager {
	return s.stateMgr
}

// GetDockerClient returns the Docker client for direct access when needed.
func (s *EnvironmentService) GetDockerClient() *docker.Client {
	return s.dockerClient
}

// GetWorkspacePath returns the workspace path.
func (s *EnvironmentService) GetWorkspacePath() string {
	return s.workspacePath
}
