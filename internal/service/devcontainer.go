package service

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/env"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lifecycle"
	"github.com/griffithind/dcx/internal/lockfile"
	"github.com/griffithind/dcx/internal/secrets"
	"github.com/griffithind/dcx/internal/ssh/deploy"
	"github.com/griffithind/dcx/internal/ssh/hostconfig"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
)

// DevContainerService provides high-level operations for devcontainer environments.
// This replaces the previous EnvironmentService in internal/orchestrator.
type DevContainerService struct {
	logger        *slog.Logger
	stateManager  *state.StateManager
	builder       *devcontainer.Builder
	workspacePath string
	configPath    string
	verbose       bool

	// Cached resolved devcontainer from last operation
	lastResolved *devcontainer.ResolvedDevContainer
}

// NewDevContainerService creates a new devcontainer service.
func NewDevContainerService(workspacePath, configPath string, verbose bool) *DevContainerService {
	return &DevContainerService{
		logger:        slog.Default(),
		stateManager:  state.NewStateManager(container.MustDocker()),
		builder:       devcontainer.NewBuilder(slog.Default()),
		workspacePath: workspacePath,
		configPath:    configPath,
		verbose:       verbose,
	}
}

// Close releases resources held by the service.
func (s *DevContainerService) Close() {
	// No additional resources to clean up
}

// Identifiers contains the core identifiers for a workspace.
type Identifiers struct {
	ProjectName string
	WorkspaceID string
	SSHHost     string
}

// GetIdentifiers computes the core identifiers for this workspace.
// Project name is derived from the devcontainer.json name field.
func (s *DevContainerService) GetIdentifiers() (*Identifiers, error) {
	// Load devcontainer.json to get the name
	cfg, _, err := devcontainer.Load(s.workspacePath, s.configPath)
	if err != nil {
		// Fall back to workspace-based ID if config not loadable
		workspaceID := devcontainer.ComputeID(s.workspacePath)
		return &Identifiers{
			WorkspaceID: workspaceID,
			SSHHost:     workspaceID + common.SSHHostSuffix,
		}, nil
	}

	dcID := devcontainer.ComputeDevContainerID(s.workspacePath, cfg)

	return &Identifiers{
		ProjectName: dcID.ProjectName,
		WorkspaceID: dcID.ID,
		SSHHost:     dcID.SSHHost,
	}, nil
}

// GetStateManager returns the state manager for direct access when needed.
func (s *DevContainerService) GetStateManager() *state.StateManager {
	return s.stateManager
}

// UpOptions contains options for bringing up a devcontainer.
type UpOptions struct {
	// Rebuild forces a rebuild of the container image
	Rebuild bool

	// Recreate forces recreation of the container
	Recreate bool

	// Pull forces pulling base images
	Pull bool
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

// LoadOptions configures the Load operation.
type LoadOptions struct {
	// ForcePull forces re-fetching features from the registry
	ForcePull bool
	// UseLockfile loads and uses the lockfile for feature resolution
	UseLockfile bool
}

// Load resolves the devcontainer configuration.
func (s *DevContainerService) Load(ctx context.Context) (*devcontainer.ResolvedDevContainer, error) {
	return s.LoadWithOptions(ctx, LoadOptions{UseLockfile: true})
}

// LoadWithOptions resolves the devcontainer configuration with specified options.
func (s *DevContainerService) LoadWithOptions(ctx context.Context, opts LoadOptions) (*devcontainer.ResolvedDevContainer, error) {
	cfg, configPath, err := devcontainer.Load(s.workspacePath, s.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Merge image metadata if available (per spec)
	cfg = s.mergeImageMetadata(ctx, cfg)

	// Project name from devcontainer.json name field
	var projectName string
	if cfg.Name != "" {
		projectName = common.SanitizeProjectName(cfg.Name)
	}

	// Load lockfile if requested and features exist
	var lf *lockfile.Lockfile
	if opts.UseLockfile && len(cfg.Features) > 0 {
		var err error
		lf, _, err = lockfile.Load(configPath)
		if err != nil {
			// Log warning but continue without lockfile
			if s.verbose {
				ui.Warning("Failed to load lockfile: %v", err)
			}
		}
	}

	resolved, err := s.builder.Build(ctx, devcontainer.BuilderOptions{
		ConfigPath:    configPath,
		WorkspaceRoot: s.workspacePath,
		Config:        cfg,
		ProjectName:   projectName,
		Lockfile:      lf,
		ForcePull:     opts.ForcePull,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve devcontainer: %w", err)
	}

	s.lastResolved = resolved
	return resolved, nil
}

// mergeImageMetadata merges devcontainer.metadata from the base image with local config.
// Per spec, images can embed configuration in the devcontainer.metadata label.
func (s *DevContainerService) mergeImageMetadata(ctx context.Context, cfg *devcontainer.DevContainerConfig) *devcontainer.DevContainerConfig {
	// Get base image reference from config
	imageRef := cfg.Image
	if imageRef == "" {
		// For Dockerfile-based configs, we'd need to parse FROM which is complex.
		// Skip for now - image metadata is most useful for pre-built images anyway.
		return cfg
	}

	// Try to get image labels (the image may not be pulled yet)
	labels, err := container.MustDocker().GetImageLabels(ctx, imageRef)
	if err != nil {
		// Image not available locally, skip metadata merge
		// It will be pulled later during Up
		return cfg
	}

	// Look for devcontainer.metadata label
	label := labels[devcontainer.DevcontainerMetadataLabel]
	if label == "" {
		return cfg
	}

	// Parse the metadata
	imageConfigs, err := devcontainer.ParseImageMetadata(label)
	if err != nil {
		if s.verbose {
			ui.Warning("Failed to parse image metadata: %v", err)
		}
		return cfg
	}

	if len(imageConfigs) == 0 {
		return cfg
	}

	// Merge image metadata with local config (local config takes precedence)
	merged := devcontainer.MergeMetadata(cfg, imageConfigs)
	if s.verbose {
		ui.Printf("Merged configuration from %d image metadata layer(s)", len(imageConfigs))
	}

	return merged
}

// Up brings up a devcontainer environment.
func (s *DevContainerService) Up(ctx context.Context, opts UpOptions) error {
	resolved, err := s.LoadWithOptions(ctx, LoadOptions{
		ForcePull:   opts.Pull,
		UseLockfile: true,
	})
	if err != nil {
		return err
	}

	ids, _ := s.GetIdentifiers()

	// Validate host requirements
	if resolved.RawConfig != nil && resolved.RawConfig.HostRequirements != nil {
		dockerInfo, err := container.MustDocker().Info(ctx)
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

	// Check current state first to determine what actions are needed
	currentState, _, err := s.stateManager.GetStateWithProjectAndHash(
		ctx, ids.ProjectName, resolved.ID, resolved.Hashes.Config)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if s.verbose {
		ui.Printf("Current state: %s", currentState)
	}

	// Early return if already running and no rebuild/recreate requested
	if currentState == state.StateRunning && !opts.Recreate && !opts.Rebuild {
		ui.Println("Devcontainer is already running")
		return nil
	}

	// Determine if we're creating a new container (affects whether we fetch secrets)
	// Secrets are only needed when creating new containers, not when starting existing ones
	isCreatingNew := currentState == state.StateAbsent ||
		currentState == state.StateStale ||
		currentState == state.StateBroken ||
		opts.Rebuild || opts.Recreate

	// Fetch secrets only when creating new containers
	var runtimeSecrets []secrets.Secret
	var buildSecretPaths map[string]string
	var secretsCleanup func()

	if isCreatingNew {
		fetcher := secrets.NewFetcher(s.logger)

		// Fetch runtime secrets (mounted after container starts)
		if len(resolved.RuntimeSecrets) > 0 {
			ui.Println("Fetching runtime secrets...")
			runtimeSecrets, err = fetcher.FetchSecrets(ctx, resolved.RuntimeSecrets)
			if err != nil {
				return fmt.Errorf("failed to fetch secrets: %w", err)
			}
		}

		// Fetch build secrets (passed to docker build)
		if len(resolved.BuildSecrets) > 0 {
			ui.Println("Fetching build secrets...")
			buildSecrets, err := fetcher.FetchSecrets(ctx, resolved.BuildSecrets)
			if err != nil {
				return fmt.Errorf("failed to fetch build secrets: %w", err)
			}
			buildSecretPaths, secretsCleanup, err = secrets.WriteToTempFiles(buildSecrets, "dcx-build-secret")
			if err != nil {
				return fmt.Errorf("failed to write build secrets: %w", err)
			}
			defer secretsCleanup()
		}
	}

	// Handle state transitions
	var isNewEnvironment bool
	var needsRebuild bool

	switch currentState {
	case state.StateRunning:
		// Already handled early return above, this is rebuild/recreate case
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
		if err := s.create(ctx, resolved, opts.Rebuild || needsRebuild, opts.Pull, buildSecretPaths); err != nil {
			return err
		}
		isNewEnvironment = true
	case state.StateCreated:
		if err := s.start(ctx, resolved); err != nil {
			return err
		}
	}

	// Get container info for subsequent operations
	_, containerInfo, err := s.stateManager.GetStateWithProject(ctx, ids.ProjectName, resolved.ID)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}

	// Pre-deploy agent binary before lifecycle hooks
	if containerInfo != nil {
		ui.Println("Installing dcx agent...")
		if err := deploy.PreDeployAgent(ctx, containerInfo.Name); err != nil {
			return fmt.Errorf("failed to install dcx agent: %w", err)
		}
	}

	// Mount runtime secrets before lifecycle hooks
	if len(runtimeSecrets) > 0 && containerInfo != nil {
		ui.Println("Mounting secrets...")
		if err := container.MountSecretsToContainer(ctx, containerInfo.Name, runtimeSecrets, resolved.EffectiveUser); err != nil {
			return fmt.Errorf("failed to mount secrets: %w", err)
		}
	}

	// Run lifecycle hooks
	if err := s.runLifecycleHooks(ctx, resolved, containerInfo, isNewEnvironment); err != nil {
		return fmt.Errorf("lifecycle hooks failed: %w", err)
	}

	// Setup SSH server access
	if err := s.setupSSHAccess(ctx, resolved, containerInfo); err != nil {
		ui.Warning("Failed to setup SSH access: %v", err)
	}

	return nil
}

// QuickStart attempts to start an existing container without full up sequence.
func (s *DevContainerService) QuickStart(ctx context.Context, containerInfo *state.ContainerInfo, projectName, workspaceID string) error {
	if containerInfo.IsSingleContainer() {
		if err := container.MustDocker().StartContainer(ctx, containerInfo.ID); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
	} else {
		actualProject := containerInfo.GetComposeProject(projectName)
		configDir := containerInfo.GetConfigDir(s.workspacePath)
		r := container.NewUnifiedRuntimeForExistingCompose(configDir, actualProject)
		if err := r.Start(ctx); err != nil {
			return fmt.Errorf("failed to start containers: %w", err)
		}
	}
	return nil
}

// create creates a new environment.
func (s *DevContainerService) create(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, forceRebuild, forcePull bool, buildSecrets map[string]string) error {
	runtime, err := container.NewUnifiedRuntime(resolved)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	if _, ok := resolved.Plan.(*devcontainer.ComposePlan); ok {
		ui.Println("Creating compose-based devcontainer...")
	} else {
		ui.Println("Creating single-container devcontainer...")
	}

	return runtime.Up(ctx, container.UpOptions{
		Build:        forceRebuild,
		Rebuild:      forceRebuild,
		Pull:         forcePull,
		BuildSecrets: buildSecrets,
	})
}

// start starts an existing stopped environment.
func (s *DevContainerService) start(ctx context.Context, resolved *devcontainer.ResolvedDevContainer) error {
	ui.Println("Starting existing devcontainer...")

	runtime, err := container.NewUnifiedRuntime(resolved)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	return runtime.Start(ctx)
}

// runLifecycleHooks runs appropriate lifecycle hooks.
func (s *DevContainerService) runLifecycleHooks(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, containerInfo *state.ContainerInfo, isNew bool) error {
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Apply environment patches and probing before lifecycle hooks
	probedEnv, err := s.setupContainerEnvironment(ctx, resolved, containerInfo)
	if err != nil {
		ui.Warning("Environment setup failed: %v", err)
		// Continue with hooks even if env setup fails
	}

	hookRunner := lifecycle.NewHookRunner(
		containerInfo.ID,
		s.workspacePath,
		resolved.RawConfig,
		resolved.ID,
	)

	// Set probed environment for hook execution
	if probedEnv != nil {
		hookRunner.SetProbedEnv(probedEnv)
	}

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

// setupContainerEnvironment applies patches and probes the user environment.
// Returns the probed environment variables to be injected into lifecycle hooks.
func (s *DevContainerService) setupContainerEnvironment(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, containerInfo *state.ContainerInfo) (map[string]string, error) {
	cfg := resolved.RawConfig
	if cfg == nil {
		return nil, nil
	}

	patcher := env.NewPatcher()
	prober := env.NewProber()

	// Collect environment variables to patch into /etc/environment
	envToPatch := make(map[string]string)
	for k, v := range cfg.ContainerEnv {
		envToPatch[k] = v
	}
	for k, v := range cfg.RemoteEnv {
		envToPatch[k] = v
	}

	// Patch /etc/environment if there are env vars to write
	if len(envToPatch) > 0 {
		if s.verbose {
			ui.Printf("  [env] Patching /etc/environment with %d variables...", len(envToPatch))
		}
		if err := patcher.PatchEtcEnvironment(ctx, containerInfo.ID, envToPatch); err != nil {
			ui.Warning("Failed to patch /etc/environment: %v", err)
		}
	}

	// Patch /etc/profile to preserve PATH from features
	if s.verbose {
		ui.Println("  [env] Patching /etc/profile for PATH preservation...")
	}
	if err := patcher.PatchEtcProfile(ctx, containerInfo.ID); err != nil {
		ui.Warning("Failed to patch /etc/profile: %v", err)
	}

	// Probe user environment if configured
	if cfg.UserEnvProbe == "" || cfg.UserEnvProbe == "none" {
		return nil, nil
	}

	probeType := env.ParseProbeType(cfg.UserEnvProbe)
	if probeType == env.ProbeNone {
		return nil, nil
	}

	// Determine user for probing
	user := cfg.RemoteUser
	if user == "" {
		user = cfg.ContainerUser
	}

	// Use derived image hash for caching
	imageHash := ""
	if resolved.Hashes != nil {
		imageHash = resolved.Hashes.Config
	}

	if s.verbose {
		ui.Printf("  [env] Probing user environment (mode: %s)...", cfg.UserEnvProbe)
	}

	probedEnv, err := prober.ProbeWithCache(ctx, containerInfo.ID, probeType, user, imageHash)
	if err != nil {
		return nil, fmt.Errorf("environment probe failed: %w", err)
	}

	if s.verbose && probedEnv != nil {
		ui.Printf("  [env] Captured %d environment variables", len(probedEnv))
	}

	return probedEnv, nil
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

	// Determine user - use EffectiveUser which is already resolved and substituted
	user := resolved.EffectiveUser
	if user == "" {
		user = "root"
	}

	// Use project name as SSH host if available
	ids, _ := s.GetIdentifiers()
	hostName := ids.SSHHost
	if err := hostconfig.AddSSHConfig(hostName, containerInfo.Name, user); err != nil {
		return fmt.Errorf("failed to update SSH config: %w", err)
	}

	ui.Printf("SSH configured: ssh %s", hostName)
	return nil
}

// DownOptions contains options for tearing down a devcontainer.
type DownOptions struct {
	RemoveVolumes bool
	RemoveOrphans bool
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
	if containerInfo.IsSingleContainer() {
		if containerInfo.Running {
			if err := container.MustDocker().StopContainer(ctx, containerInfo.ID, nil); err != nil {
				return fmt.Errorf("failed to stop container: %w", err)
			}
		}
		if err := container.MustDocker().RemoveContainer(ctx, containerInfo.ID, true, opts.RemoveVolumes); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
	} else {
		actualProject := containerInfo.GetComposeProject(projectName)
		configDir := containerInfo.GetConfigDir(s.workspacePath)
		r := container.NewUnifiedRuntimeForExistingCompose(configDir, actualProject)
		if err := r.Down(ctx, container.DownOptions{
			RemoveVolumes: opts.RemoveVolumes,
			RemoveOrphans: opts.RemoveOrphans,
		}); err != nil {
			return fmt.Errorf("failed to remove environment: %w", err)
		}
	}

	// Clean up SSH config entry
	if containerInfo != nil {
		_ = hostconfig.RemoveSSHConfig(containerInfo.Name)
	}

	ui.Println("Devcontainer removed")
	return nil
}

// BuildOptions configures the Build operation.
type BuildOptions struct {
	NoCache bool
	Pull    bool

	// UpdateLockfile updates the lockfile after successful build
	UpdateLockfile bool
	// FrozenLockfile fails if lockfile doesn't match resolved features
	FrozenLockfile bool
}

// LockMode specifies the lockfile operation mode.
type LockMode int

const (
	// LockModeGenerate creates or updates the lockfile
	LockModeGenerate LockMode = iota
	// LockModeVerify checks if lockfile matches without updating
	LockModeVerify
	// LockModeFrozen fails if lockfile doesn't exist or doesn't match
	LockModeFrozen
)

// LockOptions configures the Lock operation.
type LockOptions struct {
	Mode LockMode
}

// LockAction describes what action was taken.
type LockAction int

const (
	LockActionCreated LockAction = iota
	LockActionUpdated
	LockActionVerified
	LockActionNoChange
	LockActionNoFeatures
)

// LockResult contains the result of a lock operation.
type LockResult struct {
	Action       LockAction
	LockfilePath string
	FeatureCount int
	Changes      []string
}

// Build builds the devcontainer images without starting containers.
func (s *DevContainerService) Build(ctx context.Context, opts BuildOptions) error {
	resolved, err := s.LoadWithOptions(ctx, LoadOptions{
		ForcePull:   opts.Pull,
		UseLockfile: !opts.FrozenLockfile, // Don't use lockfile if frozen (verify mode)
	})
	if err != nil {
		return err
	}

	runtime, err := container.NewUnifiedRuntime(resolved)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}

	return runtime.Build(ctx, container.BuildOptions{
		NoCache: opts.NoCache,
		Pull:    opts.Pull,
	})
}

// Lock generates, verifies, or checks the devcontainer-lock.json file.
func (s *DevContainerService) Lock(ctx context.Context, opts LockOptions) (*LockResult, error) {
	// Load and resolve the devcontainer configuration
	cfg, configPath, err := devcontainer.Load(s.workspacePath, s.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Check if there are any features to lock
	if len(cfg.Features) == 0 {
		return &LockResult{
			Action:       LockActionNoFeatures,
			LockfilePath: lockfile.GetPath(configPath),
		}, nil
	}

	// Load existing lockfile
	existingLockfile, initMarker, err := lockfile.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load existing lockfile: %w", err)
	}

	// For frozen mode, require existing lockfile
	if opts.Mode == LockModeFrozen && existingLockfile == nil && !initMarker {
		return nil, fmt.Errorf("lockfile not found: run 'dcx lock' to generate one")
	}

	// Create feature manager and resolve features
	configDir := filepath.Dir(configPath)
	mgr, err := features.NewManager(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create feature manager: %w", err)
	}

	// For verify/frozen modes, use existing lockfile for resolution
	// This ensures we're checking against what the lockfile says
	if opts.Mode != LockModeGenerate && existingLockfile != nil {
		mgr.SetLockfile(existingLockfile)
	}

	// Resolve all features
	var overrideOrder []string
	if cfg.OverrideFeatureInstallOrder != nil {
		overrideOrder = cfg.OverrideFeatureInstallOrder
	}

	resolvedFeatures, err := mgr.ResolveAll(ctx, cfg.Features, overrideOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve features: %w", err)
	}

	// Generate new lockfile from resolved features
	newLockfile := features.GenerateLockfile(resolvedFeatures)
	lockfilePath := lockfile.GetPath(configPath)

	// Handle based on mode
	switch opts.Mode {
	case LockModeVerify:
		mismatches := features.VerifyLockfile(resolvedFeatures, existingLockfile)
		if len(mismatches) > 0 {
			changes := make([]string, len(mismatches))
			for i, m := range mismatches {
				changes[i] = m.Message
			}
			return nil, fmt.Errorf("lockfile verification failed:\n  - %s", joinStrings(changes, "\n  - "))
		}
		return &LockResult{
			Action:       LockActionVerified,
			LockfilePath: lockfilePath,
			FeatureCount: len(newLockfile.Features),
		}, nil

	case LockModeFrozen:
		mismatches := features.VerifyLockfile(resolvedFeatures, existingLockfile)
		if len(mismatches) > 0 {
			changes := make([]string, len(mismatches))
			for i, m := range mismatches {
				changes[i] = m.Message
			}
			return nil, fmt.Errorf("lockfile mismatch (frozen mode):\n  - %s", joinStrings(changes, "\n  - "))
		}
		return &LockResult{
			Action:       LockActionVerified,
			LockfilePath: lockfilePath,
			FeatureCount: len(newLockfile.Features),
		}, nil

	default: // LockModeGenerate
		// Check if lockfile needs updating
		if existingLockfile != nil && existingLockfile.Equals(newLockfile) {
			return &LockResult{
				Action:       LockActionNoChange,
				LockfilePath: lockfilePath,
				FeatureCount: len(newLockfile.Features),
			}, nil
		}

		// Collect changes for reporting
		var changes []string
		if existingLockfile != nil {
			mismatches := features.VerifyLockfile(resolvedFeatures, existingLockfile)
			for _, m := range mismatches {
				changes = append(changes, m.Message)
			}
		}

		// Save the new lockfile
		if err := newLockfile.Save(configPath); err != nil {
			return nil, fmt.Errorf("failed to save lockfile: %w", err)
		}

		action := LockActionUpdated
		if existingLockfile == nil || initMarker {
			action = LockActionCreated
		}

		return &LockResult{
			Action:       action,
			LockfilePath: lockfilePath,
			FeatureCount: len(newLockfile.Features),
			Changes:      changes,
		}, nil
	}
}

// joinStrings joins strings with a separator.
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for _, s := range strs[1:] {
		result += sep + s
	}
	return result
}
