// Package service provides high-level orchestration for devcontainer environments.
// It abstracts the differences between compose and single-container runners,
// and coordinates config loading, state management, and lifecycle hooks.
package service

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/lifecycle"
	runnerPkg "github.com/griffithind/dcx/internal/runner"
	sshcontainer "github.com/griffithind/dcx/internal/ssh/container"
	"github.com/griffithind/dcx/internal/ssh/host"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/griffithind/dcx/internal/workspace"
)

// EnvironmentService orchestrates devcontainer environment operations.
type EnvironmentService struct {
	dockerClient  *docker.Client
	stateMgr      *container.Manager
	workspacePath string
	configPath    string // optional override
	verbose       bool

	// lastWorkspace holds the workspace from the most recent create/start operation.
	// This provides access to resolved features for lifecycle hooks without re-resolution.
	lastWorkspace *workspace.Workspace
}

// NewEnvironmentService creates a new environment service.
func NewEnvironmentService(dockerClient *docker.Client, workspacePath, configPath string, verbose bool) *EnvironmentService {
	return &EnvironmentService{
		dockerClient:  dockerClient,
		stateMgr:      container.NewManager(dockerClient),
		workspacePath: workspacePath,
		configPath:    configPath,
		verbose:       verbose,
	}
}

// Identifiers contains the core identifiers for a workspace.
// This is a lightweight struct that can be computed without loading the full config.
type Identifiers struct {
	// ProjectName is the sanitized project name from dcx.json (may be empty)
	ProjectName string
	// WorkspaceID is the stable workspace identifier (hash of workspace path)
	WorkspaceID string
	// SSHHost is the SSH hostname for this workspace (projectName.dcx or workspaceID.dcx)
	SSHHost string
}

// GetIdentifiers computes the core identifiers for this workspace.
// This is a lightweight operation that doesn't require loading devcontainer.json.
func (s *EnvironmentService) GetIdentifiers() (*Identifiers, error) {
	// Load dcx.json configuration (optional)
	dcxCfg, _ := config.LoadDcxConfig(s.workspacePath)

	// Get project name from dcx.json
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = docker.SanitizeProjectName(dcxCfg.Name)
	}

	// Compute workspace ID
	workspaceID := workspace.ComputeID(s.workspacePath)

	// Compute SSH host
	sshHost := workspaceID
	if projectName != "" {
		sshHost = projectName
	}
	sshHost = sshHost + ".dcx"

	return &Identifiers{
		ProjectName: projectName,
		WorkspaceID:      workspaceID,
		SSHHost:     sshHost,
	}, nil
}

// EnvironmentInfo contains resolved environment configuration.
type EnvironmentInfo struct {
	Config      *config.DevContainerConfig
	ConfigPath  string
	DcxConfig   *config.DcxConfig
	ProjectName string
	WorkspaceID      string
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
		ui.Printf("Loaded configuration from: %s", cfgPath)
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
			ui.Printf("Project name: %s", projectName)
		}
	}

	// Validate configuration
	if err := config.Validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	// Compute identifiers
	workspaceID := workspace.ComputeID(s.workspacePath)
	// Use simple hash of raw JSON to match workspace builder
	var configHash string
	if raw := cfg.GetRawJSON(); len(raw) > 0 {
		configHash = config.ComputeSimpleHash(raw)
	}

	if s.verbose {
		ui.Printf("Env key: %s", workspaceID)
		ui.Printf("Config hash: %s", configHash[:12])
	}

	return &EnvironmentInfo{
		Config:      cfg,
		ConfigPath:  cfgPath,
		DcxConfig:   dcxCfg,
		ProjectName: projectName,
		WorkspaceID:      workspaceID,
		ConfigHash:  configHash,
	}, nil
}

// GetState returns the current state of the environment.
func (s *EnvironmentService) GetState(ctx context.Context, info *EnvironmentInfo) (container.State, *container.ContainerInfo, error) {
	return s.stateMgr.GetStateWithProjectAndHash(ctx, info.ProjectName, info.WorkspaceID, info.ConfigHash)
}

// GetStateBasic returns the current state without hash checking.
func (s *EnvironmentService) GetStateBasic(ctx context.Context, projectName, workspaceID string) (container.State, *container.ContainerInfo, error) {
	return s.stateMgr.GetStateWithProject(ctx, projectName, workspaceID)
}

// CreateRunner creates the unified runner for all configuration types.
// The unified runner handles both compose and single-container plans.
// The workspace is stored on the service for access by lifecycle hooks.
func (s *EnvironmentService) CreateRunner(info *EnvironmentInfo) (runnerPkg.Environment, error) {
	// Build a workspace for the unified runner (works for both compose and single)
	ws, err := s.buildWorkspace(info)
	if err != nil {
		return nil, fmt.Errorf("failed to build workspace: %w", err)
	}

	// Store workspace for access by lifecycle hooks (features are pre-resolved)
	s.lastWorkspace = ws

	r, err := runnerPkg.NewUnifiedRunner(ws, s.dockerClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create runner: %w", err)
	}
	return r, nil
}

// buildWorkspace constructs a workspace from environment info.
func (s *EnvironmentService) buildWorkspace(info *EnvironmentInfo) (*workspace.Workspace, error) {
	builder := workspace.NewBuilder(nil)
	return builder.Build(context.Background(), workspace.BuilderOptions{
		ConfigPath:    info.ConfigPath,
		WorkspaceRoot: s.workspacePath,
		Config:        info.Config,
		ProjectName:   info.ProjectName,
	})
}

// PlanAction represents the action to be taken.
type PlanAction string

const (
	PlanActionNone     PlanAction = "none"
	PlanActionStart    PlanAction = "start"
	PlanActionCreate   PlanAction = "create"
	PlanActionRecreate PlanAction = "recreate"
	PlanActionRebuild  PlanAction = "rebuild"
)

// PlanOptions configures the Plan operation.
type PlanOptions struct {
	Recreate bool
	Rebuild  bool
}

// PlanResult contains the result of planning what action to take.
type PlanResult struct {
	Info          *EnvironmentInfo
	State         container.State
	ContainerInfo *container.ContainerInfo
	Action        PlanAction
	Reason        string
	Changes       []string
}

// Plan analyzes the current state and determines what action would be taken.
// This is the single source of truth for "what action to take" decisions.
func (s *EnvironmentService) Plan(ctx context.Context, opts PlanOptions) (*PlanResult, error) {
	info, err := s.LoadEnvironmentInfo()
	if err != nil {
		return nil, err
	}

	currentState, containerInfo, err := s.stateMgr.GetStateWithProjectAndHash(
		ctx, info.ProjectName, info.WorkspaceID, info.ConfigHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %w", err)
	}

	result := &PlanResult{
		Info:          info,
		State:         currentState,
		ContainerInfo: containerInfo,
	}

	// Determine action based on current state
	switch currentState {
	case container.StateRunning:
		if opts.Rebuild {
			result.Action = PlanActionRebuild
			result.Reason = "force rebuild requested"
		} else if opts.Recreate {
			result.Action = PlanActionRecreate
			result.Reason = "force recreate requested"
		} else {
			result.Action = PlanActionNone
			result.Reason = "container is running and up to date"
		}
	case container.StateStale:
		result.Action = PlanActionRecreate
		result.Reason = "configuration changed"
		result.Changes = []string{"devcontainer.json modified"}
	case container.StateBroken:
		result.Action = PlanActionRecreate
		result.Reason = "container state is broken"
	case container.StateAbsent:
		result.Action = PlanActionCreate
		result.Reason = "no container found"
	case container.StateCreated:
		result.Action = PlanActionStart
		result.Reason = "container exists but stopped"
	}

	return result, nil
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
			ui.Warning("Could not get Docker info for resource validation: %v", err)
			// Fall back to host-based check
			result := config.ValidateHostRequirements(info.Config.HostRequirements)
			for _, warning := range result.Warnings {
				ui.Warning("%s", warning)
			}
			if !result.Satisfied {
				for _, errMsg := range result.Errors {
					ui.Error("%s", errMsg)
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
				ui.Warning("%s", warning)
			}
			if !result.Satisfied {
				for _, errMsg := range result.Errors {
					ui.Error("%s", errMsg)
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
		ui.Printf("Current state: %s", currentState)
	}

	// Handle state transitions
	var isNewEnvironment bool
	var needsRebuild bool

	switch currentState {
	case container.StateRunning:
		if !opts.Recreate && !opts.Rebuild {
			ui.Println("Devcontainer is already running")
			return nil
		}
		fallthrough
	case container.StateStale, container.StateBroken:
		if s.verbose {
			ui.Println("Removing existing devcontainer...")
		}
		if err := s.Down(ctx, info, DownOptions{RemoveVolumes: true}); err != nil {
			return fmt.Errorf("failed to remove existing environment: %w", err)
		}
		needsRebuild = true
		fallthrough
	case container.StateAbsent:
		if err := s.create(ctx, info, opts.Rebuild || needsRebuild, opts.Pull); err != nil {
			return err
		}
		isNewEnvironment = true
	case container.StateCreated:
		if err := s.start(ctx, info); err != nil {
			return err
		}
	}

	// Get container info for subsequent operations
	_, containerInfo, err = s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.WorkspaceID)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	// Note: UID/GID update is now done at build time (in runner.go) per devcontainer spec.
	// This ensures the UID is baked into the image layer for better caching and compatibility.

	// Pre-deploy agent binary before lifecycle hooks if SSH agent is enabled
	if opts.SSHAgentEnabled && containerInfo != nil {
		ui.Println("Installing dcx agent...")
		if err := sshcontainer.PreDeployAgent(ctx, containerInfo.Name); err != nil {
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
			ui.Warning("Failed to setup SSH access: %v", err)
		}
	}

	return nil
}

// QuickStart attempts to start an existing container without full up sequence.
// Returns (true, nil) if quick start succeeded, (false, nil) if full up is needed,
// or (false, error) if an error occurred.
func (s *EnvironmentService) QuickStart(ctx context.Context, containerInfo *container.ContainerInfo, projectName, workspaceID string) error {
	// Determine plan type (single-container vs compose)
	isSingleContainer := containerInfo != nil && (containerInfo.Plan == labels.BuildMethodImage ||
		containerInfo.Plan == labels.BuildMethodDockerfile)
	if isSingleContainer {
		// Single container - use Docker API directly
		if err := s.dockerClient.StartContainer(ctx, containerInfo.ID); err != nil {
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
		r := runnerPkg.NewUnifiedRunnerForExisting(s.workspacePath, actualProject, workspaceID)
		if err := r.Start(ctx); err != nil {
			return fmt.Errorf("failed to start containers: %w", err)
		}
	}
	return nil
}

// create creates a new environment.
func (s *EnvironmentService) create(ctx context.Context, info *EnvironmentInfo, forceRebuild, forcePull bool) error {
	envRunner, err := s.CreateRunner(info)
	if err != nil {
		return err
	}

	if info.Config.IsComposePlan() {
		ui.Println("Creating compose-based devcontainer...")
	} else {
		ui.Println("Creating single-container devcontainer...")
	}

	return envRunner.Up(ctx, runnerPkg.UpOptions{
		Build:   forceRebuild,
		Rebuild: forceRebuild,
		Pull:    forcePull,
	})
}

// start starts an existing stopped environment.
func (s *EnvironmentService) start(ctx context.Context, info *EnvironmentInfo) error {
	ui.Println("Starting existing devcontainer...")

	envRunner, err := s.CreateRunner(info)
	if err != nil {
		return err
	}

	return envRunner.Start(ctx)
}

// DownOptions is an alias for runner.DownOptions.
type DownOptions = runnerPkg.DownOptions

// Down removes the environment.
func (s *EnvironmentService) Down(ctx context.Context, info *EnvironmentInfo, opts DownOptions) error {
	currentState, containerInfo, err := s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.WorkspaceID)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if currentState == container.StateAbsent {
		ui.Println("No devcontainer found")
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
		r := runnerPkg.NewUnifiedRunnerForExisting(s.workspacePath, actualProject, info.WorkspaceID)
		if err := r.Down(ctx, runnerPkg.DownOptions{
			RemoveVolumes: opts.RemoveVolumes,
			RemoveOrphans: opts.RemoveOrphans,
		}); err != nil {
			return fmt.Errorf("failed to remove environment: %w", err)
		}
	}

	// Clean up SSH config entry
	if containerInfo != nil {
		host.RemoveSSHConfig(containerInfo.Name)
	}

	return nil
}

// DownWithWorkspaceID removes the environment using just project name and env key.
func (s *EnvironmentService) DownWithWorkspaceID(ctx context.Context, projectName, workspaceID string, opts DownOptions) error {
	currentState, containerInfo, err := s.stateMgr.GetStateWithProject(ctx, projectName, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if currentState == container.StateAbsent {
		ui.Println("No devcontainer found")
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
		r := runnerPkg.NewUnifiedRunnerForExisting(s.workspacePath, actualProject, workspaceID)
		if err := r.Down(ctx, runnerPkg.DownOptions{
			RemoveVolumes: opts.RemoveVolumes,
			RemoveOrphans: opts.RemoveOrphans,
		}); err != nil {
			return fmt.Errorf("failed to remove environment: %w", err)
		}
	}

	// Clean up SSH config entry
	if containerInfo != nil {
		host.RemoveSSHConfig(containerInfo.Name)
	}

	ui.Println("Devcontainer removed")
	return nil
}

// BuildOptions is an alias for runner.BuildOptions.
type BuildOptions = runnerPkg.BuildOptions

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
		ui.Println("Building compose-based devcontainer...")
	}

	if err := envRunner.Build(ctx, runnerPkg.BuildOptions{
		NoCache: opts.NoCache,
		Pull:    opts.Pull,
	}); err != nil {
		return fmt.Errorf("failed to build: %w", err)
	}

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
		ui.Println("Skipping stop: shutdownAction is set to 'none'")
		ui.Println("Use --force to stop anyway")
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
		ui.Println("  [hooks] Getting container state...")
	}
	_, containerInfo, err := s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.WorkspaceID)
	if err != nil {
		return fmt.Errorf("failed to get container state: %w", err)
	}
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}
	if s.verbose {
		ui.Printf("  [hooks] Container: %s", containerInfo.Name)
	}

	// Create hook runner
	hookRunner := lifecycle.NewHookRunner(
		s.dockerClient,
		containerInfo.ID,
		s.workspacePath,
		info.Config,
		info.WorkspaceID,
		sshAgentEnabled,
	)

	// Use pre-resolved features from workspace (resolved during CreateRunner/buildWorkspace)
	if s.lastWorkspace != nil && len(s.lastWorkspace.ResolvedFeatures) > 0 {
		resolvedFeatures := s.lastWorkspace.ResolvedFeatures
		if s.verbose {
			ui.Printf("  [hooks] Using %d pre-resolved features", len(resolvedFeatures))
		}

		// Collect feature hooks - features.FeatureHook is now the canonical type
		hookRunner.SetFeatureHooks(
			features.CollectOnCreateCommands(resolvedFeatures),
			features.CollectUpdateContentCommands(resolvedFeatures),
			features.CollectPostCreateCommands(resolvedFeatures),
			features.CollectPostStartCommands(resolvedFeatures),
			features.CollectPostAttachCommands(resolvedFeatures),
		)
	}

	// Run appropriate hooks based on whether this is a new environment
	if isNew {
		if s.verbose {
			ui.Println("  [hooks] Running create hooks...")
		}
		return hookRunner.RunAllCreateHooks(ctx)
	}
	if s.verbose {
		ui.Println("  [hooks] Running start hooks...")
	}
	return hookRunner.RunStartHooks(ctx)
}

// setupSSHAccess configures SSH access to the container.
func (s *EnvironmentService) setupSSHAccess(ctx context.Context, info *EnvironmentInfo, containerInfo *container.ContainerInfo) error {
	if containerInfo == nil {
		_, containerInfo, _ = s.stateMgr.GetStateWithProject(ctx, info.ProjectName, info.WorkspaceID)
	}
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Deploy dcx binary to container
	binaryPath := sshcontainer.GetContainerBinaryPath()
	if err := sshcontainer.DeployToContainer(ctx, containerInfo.Name, binaryPath); err != nil {
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
	hostName := info.WorkspaceID
	if info.ProjectName != "" {
		hostName = info.ProjectName
	}
	hostName = hostName + ".dcx"
	if err := host.AddSSHConfig(hostName, containerInfo.Name, user); err != nil {
		return fmt.Errorf("failed to update SSH config: %w", err)
	}

	ui.Printf("SSH configured: ssh %s", hostName)
	return nil
}

// GetStateMgr returns the state manager for direct access when needed.
func (s *EnvironmentService) GetStateMgr() *container.Manager {
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
