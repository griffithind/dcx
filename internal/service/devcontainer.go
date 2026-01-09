package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lifecycle"
	sshcontainer "github.com/griffithind/dcx/internal/ssh/container"
	"github.com/griffithind/dcx/internal/ssh/host"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
)

// DevContainerService provides high-level operations for devcontainer environments.
// This replaces the previous EnvironmentService in internal/orchestrator.
type DevContainerService struct {
	logger          *slog.Logger
	dockerClient    *container.DockerClient
	legacyDocker    *docker.Client // For lifecycle hooks (until migration complete)
	stateManager    *state.StateManager
	builder         *devcontainer.Builder
	workspacePath   string
	configPath      string
	verbose         bool

	// Cached resolved devcontainer from last operation
	lastResolved *devcontainer.ResolvedDevContainer
}

// NewDevContainerService creates a new devcontainer service.
func NewDevContainerService(dockerClient *container.DockerClient, workspacePath, configPath string, verbose bool) *DevContainerService {
	// Create a legacy docker client for lifecycle hooks
	legacyDocker, _ := docker.NewClient()

	return &DevContainerService{
		logger:        slog.Default(),
		dockerClient:  dockerClient,
		legacyDocker:  legacyDocker,
		stateManager:  state.NewStateManager(dockerClient),
		builder:       devcontainer.NewBuilder(slog.Default()),
		workspacePath: workspacePath,
		configPath:    configPath,
		verbose:       verbose,
	}
}

// Close releases resources held by the service.
func (s *DevContainerService) Close() {
	if s.legacyDocker != nil {
		s.legacyDocker.Close()
	}
}

// Identifiers contains the core identifiers for a workspace.
type Identifiers struct {
	ProjectName string
	WorkspaceID string
	SSHHost     string
}

// GetIdentifiers computes the core identifiers for this workspace.
func (s *DevContainerService) GetIdentifiers() (*Identifiers, error) {
	dcxCfg, _ := devcontainer.LoadDcxConfig(s.workspacePath)

	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = container.SanitizeProjectName(dcxCfg.Name)
	}

	workspaceID := devcontainer.ComputeID(s.workspacePath)

	sshHost := workspaceID
	if projectName != "" {
		sshHost = projectName
	}
	sshHost = sshHost + ".dcx"

	return &Identifiers{
		ProjectName: projectName,
		WorkspaceID: workspaceID,
		SSHHost:     sshHost,
	}, nil
}

// GetStateManager returns the state manager for direct access when needed.
func (s *DevContainerService) GetStateManager() *state.StateManager {
	return s.stateManager
}

// GetDockerClient returns the Docker client for direct access when needed.
func (s *DevContainerService) GetDockerClient() *container.DockerClient {
	return s.dockerClient
}

// GetWorkspacePath returns the workspace path.
func (s *DevContainerService) GetWorkspacePath() string {
	return s.workspacePath
}

// UpOptions contains options for bringing up a devcontainer.
type UpOptions struct {
	// Rebuild forces a rebuild of the container image
	Rebuild bool

	// Recreate forces recreation of the container
	Recreate bool

	// Pull forces pulling base images
	Pull bool

	// SSHAgentEnabled enables SSH agent forwarding during lifecycle hooks
	SSHAgentEnabled bool

	// EnableSSH enables SSH server access to the container
	EnableSSH bool
}

// PlanOptions configures the Plan operation.
type PlanOptions struct {
	Recreate bool
	Rebuild  bool
}

// PlanResult contains the result of planning what action to take.
type PlanResult struct {
	Resolved      *devcontainer.ResolvedDevContainer
	State         state.ContainerState
	ContainerInfo *state.ContainerInfo
	Action        state.PlanAction
	Reason        string
	Changes       []string
}

// Plan analyzes the current state and determines what action would be taken.
func (s *DevContainerService) Plan(ctx context.Context, opts PlanOptions) (*PlanResult, error) {
	resolved, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}

	ids, _ := s.GetIdentifiers()
	currentState, containerInfo, err := s.stateManager.GetStateWithProjectAndHash(
		ctx, ids.ProjectName, resolved.ID, resolved.Hashes.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %w", err)
	}

	actionResult := state.DeterminePlanAction(currentState, opts.Rebuild, opts.Recreate)

	return &PlanResult{
		Resolved:      resolved,
		State:         currentState,
		ContainerInfo: containerInfo,
		Action:        actionResult.Action,
		Reason:        actionResult.Reason,
		Changes:       actionResult.Changes,
	}, nil
}

// Load resolves the devcontainer configuration.
func (s *DevContainerService) Load(ctx context.Context) (*devcontainer.ResolvedDevContainer, error) {
	cfg, configPath, err := devcontainer.Load(s.workspacePath, s.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	dcxCfg, _ := devcontainer.LoadDcxConfig(s.workspacePath)
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = container.SanitizeProjectName(dcxCfg.Name)
	}

	resolved, err := s.builder.Build(ctx, devcontainer.BuilderOptions{
		ConfigPath:    configPath,
		WorkspaceRoot: s.workspacePath,
		Config:        cfg,
		ProjectName:   projectName,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve devcontainer: %w", err)
	}

	s.lastResolved = resolved
	return resolved, nil
}

// Up brings up a devcontainer environment.
func (s *DevContainerService) Up(ctx context.Context, opts UpOptions) error {
	resolved, err := s.Load(ctx)
	if err != nil {
		return err
	}

	ids, _ := s.GetIdentifiers()

	// Validate host requirements
	if resolved.RawConfig != nil && resolved.RawConfig.HostRequirements != nil {
		dockerInfo, err := s.dockerClient.Info(ctx)
		if err != nil {
			ui.Warning("Could not get Docker info for resource validation: %v", err)
		} else {
			dockerRes := &devcontainer.DockerResources{
				CPUs:   dockerInfo.NCPU,
				Memory: dockerInfo.MemTotal,
			}
			result := devcontainer.ValidateHostRequirementsWithDocker(resolved.RawConfig.HostRequirements, dockerRes)
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
	currentState, containerInfo, err := s.stateManager.GetStateWithProjectAndHash(
		ctx, ids.ProjectName, resolved.ID, resolved.Hashes.Config)
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
	case state.StateRunning:
		if !opts.Recreate && !opts.Rebuild {
			ui.Println("Devcontainer is already running")
			return nil
		}
		fallthrough
	case state.StateStale, state.StateBroken:
		if s.verbose {
			ui.Println("Removing existing devcontainer...")
		}
		if err := s.DownWithIDs(ctx, ids.ProjectName, resolved.ID, DownOptions{RemoveVolumes: true}); err != nil {
			return fmt.Errorf("failed to remove existing environment: %w", err)
		}
		needsRebuild = true
		fallthrough
	case state.StateAbsent:
		if err := s.create(ctx, resolved, opts.Rebuild || needsRebuild, opts.Pull); err != nil {
			return err
		}
		isNewEnvironment = true
	case state.StateCreated:
		if err := s.start(ctx, resolved); err != nil {
			return err
		}
	}

	// Get container info for subsequent operations
	_, containerInfo, err = s.stateManager.GetStateWithProject(ctx, ids.ProjectName, resolved.ID)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	// Pre-deploy agent binary before lifecycle hooks if SSH agent is enabled
	if opts.SSHAgentEnabled && containerInfo != nil {
		ui.Println("Installing dcx agent...")
		if err := sshcontainer.PreDeployAgent(ctx, containerInfo.Name); err != nil {
			return fmt.Errorf("failed to install dcx agent: %w", err)
		}
	}

	// Run lifecycle hooks
	if err := s.runLifecycleHooks(ctx, resolved, containerInfo, isNewEnvironment, opts.SSHAgentEnabled); err != nil {
		return fmt.Errorf("lifecycle hooks failed: %w", err)
	}

	// Setup SSH server access if requested
	if opts.EnableSSH {
		if err := s.setupSSHAccess(ctx, resolved, containerInfo); err != nil {
			ui.Warning("Failed to setup SSH access: %v", err)
		}
	}

	return nil
}

// QuickStart attempts to start an existing container without full up sequence.
func (s *DevContainerService) QuickStart(ctx context.Context, containerInfo *state.ContainerInfo, projectName, workspaceID string) error {
	isSingleContainer := containerInfo != nil && (containerInfo.Plan == state.BuildMethodImage ||
		containerInfo.Plan == state.BuildMethodDockerfile)
	if isSingleContainer {
		if err := s.dockerClient.StartContainer(ctx, containerInfo.ID); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
	} else {
		actualProject := ""
		if containerInfo != nil {
			actualProject = containerInfo.ComposeProject
		}
		if actualProject == "" {
			actualProject = projectName
		}
		// Get config directory for compose commands
		configDir := s.workspacePath
		if containerInfo != nil && containerInfo.Labels != nil && containerInfo.Labels.ConfigPath != "" {
			configDir = filepath.Dir(containerInfo.Labels.ConfigPath)
		}
		r := container.NewUnifiedRuntimeForExistingCompose(configDir, actualProject, s.dockerClient)
		if err := r.Start(ctx); err != nil {
			return fmt.Errorf("failed to start containers: %w", err)
		}
	}
	return nil
}

// create creates a new environment.
func (s *DevContainerService) create(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, forceRebuild, forcePull bool) error {
	runtime, err := container.NewUnifiedRuntime(resolved, s.dockerClient)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	if _, ok := resolved.Plan.(*devcontainer.ComposePlan); ok {
		ui.Println("Creating compose-based devcontainer...")
	} else {
		ui.Println("Creating single-container devcontainer...")
	}

	return runtime.Up(ctx, container.UpOptions{
		Build:   forceRebuild,
		Rebuild: forceRebuild,
		Pull:    forcePull,
	})
}

// start starts an existing stopped environment.
func (s *DevContainerService) start(ctx context.Context, resolved *devcontainer.ResolvedDevContainer) error {
	ui.Println("Starting existing devcontainer...")

	runtime, err := container.NewUnifiedRuntime(resolved, s.dockerClient)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	return runtime.Start(ctx)
}

// runLifecycleHooks runs appropriate lifecycle hooks.
func (s *DevContainerService) runLifecycleHooks(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, containerInfo *state.ContainerInfo, isNew bool, sshAgentEnabled bool) error {
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Use legacy docker client for lifecycle hooks (until lifecycle package is migrated)
	hookRunner := lifecycle.NewHookRunner(
		s.legacyDocker,
		containerInfo.ID,
		s.workspacePath,
		resolved.RawConfig,
		resolved.ID,
		sshAgentEnabled,
	)

	// Use pre-resolved features
	if len(resolved.Features) > 0 {
		if s.verbose {
			ui.Printf("  [hooks] Using %d pre-resolved features", len(resolved.Features))
		}

		hookRunner.SetFeatureHooks(
			features.CollectOnCreateCommands(resolved.Features),
			features.CollectUpdateContentCommands(resolved.Features),
			features.CollectPostCreateCommands(resolved.Features),
			features.CollectPostStartCommands(resolved.Features),
			features.CollectPostAttachCommands(resolved.Features),
		)
	}

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
func (s *DevContainerService) setupSSHAccess(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, containerInfo *state.ContainerInfo) error {
	if containerInfo == nil {
		ids, _ := s.GetIdentifiers()
		_, containerInfo, _ = s.stateManager.GetStateWithProject(ctx, ids.ProjectName, resolved.ID)
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
	if resolved.RawConfig != nil {
		if resolved.RawConfig.RemoteUser != "" {
			user = resolved.RawConfig.RemoteUser
		} else if resolved.RawConfig.ContainerUser != "" {
			user = resolved.RawConfig.ContainerUser
		}
		user = devcontainer.Substitute(user, &devcontainer.SubstitutionContext{
			LocalWorkspaceFolder: s.workspacePath,
		})
	}

	// Use project name as SSH host if available
	ids, _ := s.GetIdentifiers()
	hostName := ids.SSHHost
	if err := host.AddSSHConfig(hostName, containerInfo.Name, user); err != nil {
		return fmt.Errorf("failed to update SSH config: %w", err)
	}

	ui.Printf("SSH configured: ssh %s", hostName)
	return nil
}

// StartOptions contains options for starting a stopped devcontainer.
type StartOptions struct {
	WorkspaceID string
	ProjectName string
}

// Start starts a stopped devcontainer.
func (s *DevContainerService) Start(ctx context.Context, opts StartOptions) error {
	// Validate state
	if err := s.stateManager.ValidateState(ctx, opts.WorkspaceID, state.OpStart); err != nil {
		return err
	}

	// Create lightweight runtime for existing container
	runtime := container.NewUnifiedRuntimeForExisting(
		"", // workspace path not needed for start
		opts.ProjectName,
		opts.WorkspaceID,
		s.dockerClient,
	)

	return runtime.Start(ctx)
}

// StopOptions contains options for stopping a devcontainer.
type StopOptions struct {
	WorkspaceID string
	ProjectName string
}

// Stop stops a running devcontainer.
func (s *DevContainerService) Stop(ctx context.Context, opts StopOptions) error {
	// Validate state
	if err := s.stateManager.ValidateState(ctx, opts.WorkspaceID, state.OpStop); err != nil {
		return err
	}

	// Create lightweight runtime for existing container
	runtime := container.NewUnifiedRuntimeForExisting(
		"",
		opts.ProjectName,
		opts.WorkspaceID,
		s.dockerClient,
	)

	return runtime.Stop(ctx)
}

// DownOptions contains options for tearing down a devcontainer.
type DownOptions struct {
	WorkspaceID    string
	ProjectName    string
	RemoveVolumes  bool
	RemoveOrphans  bool
}

// Down tears down a devcontainer environment.
func (s *DevContainerService) Down(ctx context.Context, opts DownOptions) error {
	// Validate state
	if err := s.stateManager.ValidateState(ctx, opts.WorkspaceID, state.OpDown); err != nil {
		return err
	}

	return s.DownWithIDs(ctx, opts.ProjectName, opts.WorkspaceID, opts)
}

// DownWithIDs removes the environment using just project name and workspace ID.
func (s *DevContainerService) DownWithIDs(ctx context.Context, projectName, workspaceID string, opts DownOptions) error {
	currentState, containerInfo, err := s.stateManager.GetStateWithProject(ctx, projectName, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if currentState == state.StateAbsent {
		ui.Println("No devcontainer found")
		return nil
	}

	// Handle based on plan type (single-container vs compose)
	isSingleContainer := containerInfo != nil && (containerInfo.Plan == state.BuildMethodImage ||
		containerInfo.Plan == state.BuildMethodDockerfile)
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
		// Get config directory for compose commands
		configDir := s.workspacePath
		if containerInfo != nil && containerInfo.Labels != nil && containerInfo.Labels.ConfigPath != "" {
			configDir = filepath.Dir(containerInfo.Labels.ConfigPath)
		}
		r := container.NewUnifiedRuntimeForExistingCompose(configDir, actualProject, s.dockerClient)
		if err := r.Down(ctx, container.DownOptions{
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

// ExecOptions contains options for executing a command.
type ExecOptions struct {
	WorkspaceID string
	ProjectName string
	Command     []string
	WorkingDir  string
	User        string
	Env         []string
	TTY         bool
}

// Exec executes a command in a running devcontainer.
func (s *DevContainerService) Exec(ctx context.Context, opts ExecOptions) (int, error) {
	// Validate state
	if err := s.stateManager.ValidateState(ctx, opts.WorkspaceID, state.OpExec); err != nil {
		return -1, err
	}

	// Create lightweight runtime for existing container
	runtime := container.NewUnifiedRuntimeForExisting(
		"",
		opts.ProjectName,
		opts.WorkspaceID,
		s.dockerClient,
	)

	return runtime.Exec(ctx, opts.Command, container.ExecOptions{
		WorkingDir: opts.WorkingDir,
		User:       opts.User,
		Env:        opts.Env,
		TTY:        opts.TTY,
	})
}

// StatusOptions contains options for getting status.
type StatusOptions struct {
	WorkspaceID string
	ProjectName string
	ConfigHash  string
}

// Status returns the current state of a devcontainer.
func (s *DevContainerService) Status(ctx context.Context, opts StatusOptions) (state.ContainerState, *state.ContainerInfo, error) {
	if opts.ConfigHash != "" {
		return s.stateManager.GetStateWithProjectAndHash(ctx, opts.ProjectName, opts.WorkspaceID, opts.ConfigHash)
	}
	return s.stateManager.GetStateWithProject(ctx, opts.ProjectName, opts.WorkspaceID)
}

// GetDiagnostics returns diagnostic information for troubleshooting.
func (s *DevContainerService) GetDiagnostics(ctx context.Context, workspaceID string) (*state.Diagnostics, error) {
	return s.stateManager.GetDiagnostics(ctx, workspaceID)
}

// Cleanup removes all containers and optionally volumes for a workspace.
func (s *DevContainerService) Cleanup(ctx context.Context, workspaceID string, removeVolumes bool) error {
	return s.stateManager.Cleanup(ctx, workspaceID, removeVolumes)
}

// BuildOptions configures the Build operation.
type BuildOptions struct {
	NoCache bool
	Pull    bool
}

// Build builds the devcontainer images without starting containers.
func (s *DevContainerService) Build(ctx context.Context, opts BuildOptions) error {
	resolved, err := s.Load(ctx)
	if err != nil {
		return err
	}

	runtime, err := container.NewUnifiedRuntime(resolved, s.dockerClient)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	return runtime.Build(ctx, container.BuildOptions{
		NoCache: opts.NoCache,
		Pull:    opts.Pull,
	})
}
