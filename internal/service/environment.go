// Package service provides high-level orchestration for devcontainer environments.
// It abstracts the differences between compose and single-container runners,
// and coordinates config loading, state management, and lifecycle hooks.
package service

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/lifecycle"
	runnerPkg "github.com/griffithind/dcx/internal/runner"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/workspace"
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
		projectName = docker.SanitizeProjectName(dcxCfg.Name)
		if s.verbose {
			fmt.Printf("Project name: %s\n", projectName)
		}
	}

	// Validate configuration
	if err := config.Validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Compute identifiers
	envKey := workspace.ComputeID(s.workspacePath)
	// Use simple hash of raw JSON to match workspace builder
	var configHash string
	if raw := cfg.GetRawJSON(); len(raw) > 0 {
		configHash = config.ComputeSimpleHash(raw)
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

// CreateRunner creates the unified runner for all configuration types.
// The unified runner handles both compose and single-container plans.
func (s *EnvironmentService) CreateRunner(info *EnvironmentInfo) (runnerPkg.Environment, error) {
	// Build a workspace for the unified runner (works for both compose and single)
	ws, err := s.buildWorkspace(info)
	if err != nil {
		return nil, fmt.Errorf("failed to build workspace: %w", err)
	}

	r, err := runnerPkg.NewUnifiedRunner(ws, s.dockerClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}
	return r, nil
}

// buildWorkspace constructs a workspace from environment info.
func (s *EnvironmentService) buildWorkspace(info *EnvironmentInfo) (*workspace.Workspace, error) {
	builder := workspace.NewBuilder(nil)
	return builder.Build(context.Background(), workspace.BuildOptions{
		ConfigPath:    info.ConfigPath,
		WorkspaceRoot: s.workspacePath,
		Config:        info.Config,
		ProjectName:   info.ProjectName,
	})
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

	// Validate host requirements using Docker's actual resources
	// This accounts for Docker Desktop VM limits or cgroup restrictions
	if info.Config.HostRequirements != nil {
		dockerInfo, err := s.dockerClient.Info(ctx)
		if err != nil {
			fmt.Printf("Warning: Could not get Docker info for resource validation: %v\n", err)
			// Fall back to host-based check
			result := config.ValidateHostRequirements(info.Config.HostRequirements)
			for _, warning := range result.Warnings {
				fmt.Printf("Warning: %s\n", warning)
			}
			if !result.Satisfied {
				for _, errMsg := range result.Errors {
					fmt.Printf("Error: %s\n", errMsg)
				}
				return fmt.Errorf("host requirements not satisfied")
			}
		} else {
			dockerRes := &config.DockerResources{
				CPUs:   dockerInfo.NCPU,
				Memory: dockerInfo.MemTotal,
			}
			result := config.ValidateHostRequirementsWithDocker(info.Config.HostRequirements, dockerRes)
			for _, warning := range result.Warnings {
				fmt.Printf("Warning: %s\n", warning)
			}
			if !result.Satisfied {
				for _, errMsg := range result.Errors {
					fmt.Printf("Error: %s\n", errMsg)
				}
				return fmt.Errorf("host requirements not satisfied")
			}
		}
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

	// Get container info for subsequent operations
	_, containerInfo, err = s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.EnvKey)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	// Update remote user UID/GID to match host user (Linux only)
	// This must happen before lifecycle hooks so files created by hooks have correct ownership
	if isNewEnvironment && containerInfo != nil {
		if err := s.updateRemoteUserUID(ctx, containerInfo.ID, info.Config); err != nil {
			// Non-fatal warning - some containers may not support this
			fmt.Printf("Warning: Could not update remote user UID: %v\n", err)
		}
	}

	// Pre-deploy agent binary before lifecycle hooks if SSH agent is enabled
	if opts.SSHAgentEnabled && containerInfo != nil {
		fmt.Println("Installing dcx agent...")
		if err := ssh.PreDeployAgent(ctx, containerInfo.Name); err != nil {
			return fmt.Errorf("failed to install dcx agent: %w", err)
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

	// Handle based on plan type (single-container vs compose)
	isSingleContainer := containerInfo != nil && (containerInfo.Plan == labels.BuildMethodImage ||
		containerInfo.Plan == labels.BuildMethodDockerfile)
	if isSingleContainer {
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
		r := runnerPkg.NewUnifiedRunnerForExisting(s.workspacePath, actualProject, info.EnvKey)
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

	// Handle based on plan type (single-container vs compose)
	isSingleContainer := containerInfo != nil && (containerInfo.Plan == labels.BuildMethodImage ||
		containerInfo.Plan == labels.BuildMethodDockerfile)
	if isSingleContainer {
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
		r := runnerPkg.NewUnifiedRunnerForExisting(s.workspacePath, actualProject, envKey)
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
	if s.verbose {
		fmt.Println("  [hooks] Getting container state...")
	}
	_, containerInfo, err := s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.EnvKey)
	if err != nil {
		return fmt.Errorf("failed to get container state: %w", err)
	}
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}
	if s.verbose {
		fmt.Printf("  [hooks] Container: %s\n", containerInfo.Name)
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
		if s.verbose {
			fmt.Printf("  [hooks] Resolving %d features...\n", len(info.Config.Features))
			for id := range info.Config.Features {
				fmt.Printf("  [hooks]   - %s\n", id)
			}
		}
		configDir := filepath.Dir(info.ConfigPath)
		mgr, err := features.NewManager(configDir)
		if err == nil {
			if s.verbose {
				fmt.Println("  [hooks] Calling ResolveAll...")
			}
			resolvedFeatures, err := mgr.ResolveAll(ctx, info.Config.Features, info.Config.OverrideFeatureInstallOrder)
			if s.verbose {
				fmt.Printf("  [hooks] ResolveAll returned %d features, err=%v\n", len(resolvedFeatures), err)
			}
			if err == nil && len(resolvedFeatures) > 0 {
				var onCreateHooks, updateContentHooks, postCreateHooks, postStartHooks, postAttachHooks []lifecycle.FeatureHook

				for _, fh := range features.CollectOnCreateCommands(resolvedFeatures) {
					onCreateHooks = append(onCreateHooks, lifecycle.FeatureHook{
						FeatureID:   fh.FeatureID,
						FeatureName: fh.FeatureName,
						Command:     fh.Command,
					})
				}
				for _, fh := range features.CollectUpdateContentCommands(resolvedFeatures) {
					updateContentHooks = append(updateContentHooks, lifecycle.FeatureHook{
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
				for _, fh := range features.CollectPostAttachCommands(resolvedFeatures) {
					postAttachHooks = append(postAttachHooks, lifecycle.FeatureHook{
						FeatureID:   fh.FeatureID,
						FeatureName: fh.FeatureName,
						Command:     fh.Command,
					})
				}

				hookRunner.SetFeatureHooks(onCreateHooks, updateContentHooks, postCreateHooks, postStartHooks, postAttachHooks)
			}
		}
	}

	// Run appropriate hooks based on whether this is a new environment
	if isNew {
		if s.verbose {
			fmt.Println("  [hooks] Running create hooks...")
		}
		return hookRunner.RunAllCreateHooks(ctx)
	}
	if s.verbose {
		fmt.Println("  [hooks] Running start hooks...")
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
