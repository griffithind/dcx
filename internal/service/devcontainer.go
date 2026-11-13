package service

import (
	"context"
	"fmt"
	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/report"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/env"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lockfile"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/ssh/hostconfig"
	"github.com/griffithind/dcx/internal/state"
)

// DevContainerService provides high-level operations for devcontainer environments.
type DevContainerService struct {
	idsOnce sync.Once
	ids     *Identifiers

	engine        container.Engine
	report        report.Reporter
	logger        *slog.Logger
	stateManager  *state.StateManager
	builder       *devcontainer.Builder
	workspacePath string
	configPath    string
	verbose       bool

	// Cached resolved devcontainer from last operation
}

// NewDevContainerService creates a new devcontainer service backed by the
// given container engine.
//
// Progress messages go to rep; pass report.Discard for machine-readable
// output.
func NewDevContainerService(engine container.Engine, rep report.Reporter, workspacePath, configPath string, verbose bool) *DevContainerService {
	rep = report.OrDiscard(rep)
	return &DevContainerService{
		engine:        engine,
		report:        rep,
		logger:        slog.Default(),
		stateManager:  state.NewStateManager(engine),
		builder:       devcontainer.NewBuilder(slog.Default(), rep),
		workspacePath: workspacePath,
		configPath:    configPath,
		verbose:       verbose,
	}
}

// Identifiers contains the core identifiers for a workspace.
type Identifiers struct {
	ProjectName string
	WorkspaceID string
	SSHHost     string
}

// Identifiers returns the workspace's identifiers, computed once per
// service. The workspace ID depends only on the path; the resource name
// and SSH alias use devcontainer.json's name, so when the config cannot be
// read they fall back to the ID.
func (s *DevContainerService) Identifiers() *Identifiers {
	s.idsOnce.Do(func() {
		cfg, _, err := devcontainer.Load(s.workspacePath, s.configPath)
		if err != nil {
			id := devcontainer.ComputeID(s.workspacePath)
			s.ids = &Identifiers{WorkspaceID: id, SSHHost: id + devcontainer.SSHHostSuffix}
			return
		}
		dcID := devcontainer.ComputeDevContainerID(s.workspacePath, cfg)
		s.ids = &Identifiers{ProjectName: dcID.ProjectName, WorkspaceID: dcID.ID, SSHHost: dcID.SSHHost}
	})
	return s.ids
}

// SSHAlias returns the alias `ssh` reaches this workspace by: the first
// Host of its SSH config block (normally "<name>.dcx", or the unique
// "<container>.dcx" when another workspace already uses that name), or the
// default alias when no block exists yet.
func (s *DevContainerService) SSHAlias(ctx context.Context) string {
	ids := s.Identifiers()
	if _, info, err := s.stateManager.GetState(ctx, ids.WorkspaceID); err == nil && info != nil {
		if aliases := hostconfig.Aliases(info.Name); len(aliases) > 0 {
			return aliases[0]
		}
	}
	return ids.SSHHost
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

	// SSHBindHost is the host interface the agent SSH port is published on.
	// "" means 127.0.0.1 (loopback-only, the default). "0.0.0.0" exposes the
	// port on all interfaces, gated by SSHAllowedCIDRs at the agent level.
	SSHBindHost string

	// SSHAllowedCIDRs lists CIDRs the agent's ConnCallback accepts in
	// addition to loopback. Empty means loopback-only.
	SSHAllowedCIDRs []string
}

// LoadOptions configures the Load operation.
type LoadOptions struct {
	// ForcePull forces re-fetching features from the registry
	ForcePull bool
	// UseLockfile loads and uses the lockfile for feature resolution
	UseLockfile bool
	// PullForMetadata pulls the base image when it is not available
	// locally, so its devcontainer.metadata label (remoteUser, env,
	// mounts, ...) is merged on the first run too. Without it the first
	// `up` and later runs could resolve differently.
	PullForMetadata bool
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

	// Merge image metadata if available (per spec). The pre-merge config
	// is kept for the staleness key.
	localCfg := cfg
	cfg = s.mergeImageMetadata(ctx, cfg, opts.PullForMetadata)

	// Project name from devcontainer.json name field
	var projectName string
	if cfg.Name != "" {
		projectName = devcontainer.SanitizeProjectName(cfg.Name)
	}

	// Load lockfile if requested and features exist
	var lf *lockfile.Lockfile
	if opts.UseLockfile && len(cfg.Features) > 0 {
		var err error
		lf, _, err = lockfile.Load(configPath)
		if err != nil {
			// Log warning but continue without lockfile
			if s.verbose {
				s.report.Warnf("Failed to load lockfile: %v", err)
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
		LocalConfig:   localCfg,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve devcontainer: %w", err)
	}

	return resolved, nil
}

// mergeImageMetadata merges devcontainer.metadata from the base image with local config.
// Per spec, images can embed configuration in the devcontainer.metadata label.
func (s *DevContainerService) mergeImageMetadata(ctx context.Context, cfg *devcontainer.DevContainerConfig, pull bool) *devcontainer.DevContainerConfig {
	// Get base image reference from config
	imageRef := cfg.Image
	if imageRef == "" {
		// For Dockerfile-based configs, we'd need to parse FROM which is complex.
		// Skip for now - image metadata is most useful for pre-built images anyway.
		return cfg
	}

	labels, err := s.engine.GetImageLabels(ctx, imageRef)
	if err != nil && pull {
		s.report.Infof("Pulling image: %s", imageRef)
		if perr := s.engine.PullImageWithProgress(ctx, imageRef, io.Discard); perr != nil {
			s.report.Warnf("Could not pull %s to read its metadata: %v", imageRef, perr)
		} else {
			labels, err = s.engine.GetImageLabels(ctx, imageRef)
		}
	}
	if err != nil {
		// Image not available locally; resolve without its metadata.
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
			s.report.Warnf("Failed to parse image metadata: %v", err)
		}
		return cfg
	}

	if len(imageConfigs) == 0 {
		return cfg
	}

	// Merge image metadata with local config (local config takes precedence)
	merged := devcontainer.MergeMetadata(cfg, imageConfigs)
	if s.verbose {
		s.report.Infof("Merged configuration from %d image metadata layer(s)", len(imageConfigs))
	}

	return merged
}

// create creates a new environment.
func (s *DevContainerService) create(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, opts UpOptions, buildSecrets map[string]string) error {
	prov, err := container.NewProvisioner(s.engine, s.report, resolved)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	if _, ok := resolved.Plan.(*devcontainer.ComposePlan); ok {
		s.report.Infof("Creating compose-based devcontainer...")
	} else {
		s.report.Infof("Creating single-container devcontainer...")
	}

	return prov.Up(ctx, container.UpOptions{
		Build:        opts.Rebuild,
		Rebuild:      opts.Rebuild,
		Pull:         opts.Pull,
		BuildSecrets: buildSecrets,
		SSHBindHost:  opts.SSHBindHost,
	})
}

// startExisting starts the stopped environment found by the state lookup.
//
// It acts on what actually exists (container ID, compose project label)
// rather than on names recomputed from the current config, which may differ
// for environments created by older dcx versions or before a rename.
func (s *DevContainerService) startExisting(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, existing *state.ContainerInfo) error {
	s.report.Infof("Starting existing devcontainer...")

	if existing == nil {
		return fmt.Errorf("no existing devcontainer to start")
	}
	if existing.IsSingleContainer() {
		return s.engine.StartContainer(ctx, existing.ID)
	}

	plan, ok := resolved.Plan.(*devcontainer.ComposePlan)
	if !ok {
		return fmt.Errorf("existing compose environment but configuration is not a compose plan")
	}
	project := *plan
	if existing.ComposeProject != "" {
		project.ProjectName = existing.ComposeProject
	}
	target := *resolved
	target.Plan = &project

	prov, err := container.NewProvisioner(s.engine, s.report, &target)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}
	return prov.Start(ctx)
}

// setupContainerEnvironment applies patches and probes the user environment.
// It returns the probed environment (injected into lifecycle hooks and
// sessions) and remoteEnv with ${containerEnv:…} resolved.
func (s *DevContainerService) setupContainerEnvironment(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, containerInfo *state.ContainerInfo) (probed, remoteEnv map[string]string, err error) {
	cfg := resolved.RawConfig
	if cfg == nil {
		return nil, nil, nil
	}

	patcher := env.NewPatcher(s.engine)
	prober := env.NewProber(s.engine)

	// Collect environment variables to patch into /etc/environment
	envToPatch := make(map[string]string)
	for k, v := range resolved.ContainerEnv {
		envToPatch[k] = v
	}
	remoteEnv = resolved.RemoteEnv
	if details, err := s.engine.InspectContainer(ctx, containerInfo.ID); err == nil && details != nil {
		remoteEnv = devcontainer.ResolveContainerEnvRefs(remoteEnv, devcontainer.EnvListToMap(details.Env))
	}
	for k, v := range remoteEnv {
		envToPatch[k] = v
	}

	// Always reconcile dcx's block, so removed variables disappear too.
	if s.verbose {
		s.report.Infof("  [env] Patching /etc/environment with %d variables...", len(envToPatch))
	}
	if skipped, err := patcher.PatchEtcEnvironment(ctx, containerInfo.ID, envToPatch); err != nil {
		s.report.Warnf("Failed to patch /etc/environment: %v", err)
	} else if len(skipped) > 0 {
		s.report.Warnf("Not written to /etc/environment (invalid name or multi-line value): %s", strings.Join(skipped, ", "))
	}

	// Patch /etc/profile to preserve PATH from features
	if s.verbose {
		s.report.Infof("  [env] Patching /etc/profile for PATH preservation...")
	}
	if err := patcher.PatchEtcProfile(ctx, containerInfo.ID); err != nil {
		s.report.Warnf("Failed to patch /etc/profile: %v", err)
	}

	// Probe user environment if configured
	if cfg.UserEnvProbe == "" || cfg.UserEnvProbe == "none" {
		return nil, remoteEnv, nil
	}

	probeType := env.ParseProbeType(cfg.UserEnvProbe)
	if probeType == env.ProbeNone {
		return nil, remoteEnv, nil
	}

	// Probe as the user sessions run as (remoteUser after image metadata
	// is merged), not the raw config's value.
	user := resolved.EffectiveUser

	if s.verbose {
		s.report.Infof("  [env] Probing user environment (mode: %s)...", cfg.UserEnvProbe)
	}

	probedEnv, err := prober.Probe(ctx, containerInfo.ID, probeType, user)
	if err != nil {
		return nil, remoteEnv, fmt.Errorf("environment probe failed: %w", err)
	}

	if s.verbose && probedEnv != nil {
		s.report.Infof("  [env] Captured %d environment variables", len(probedEnv))
	}

	return probedEnv, remoteEnv, nil
}

// setupSSHAccess configures SSH access to the container.
//
// Looks up the ephemeral host port Docker assigned to the agent's 48022
// listener, pins the workspace's host key into ~/.dcx/known_hosts, and
// writes the ~/.ssh/config block pointing at 127.0.0.1:<port>.
func (s *DevContainerService) setupSSHAccess(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, containerInfo *state.ContainerInfo) error {
	if containerInfo == nil {
		_, containerInfo, _ = s.stateManager.GetState(ctx, resolved.ID)
	}
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Determine user - use EffectiveUser which is already resolved and substituted
	user := resolved.EffectiveUser
	if user == "" {
		user = "root"
	}

	ids := s.Identifiers()

	// Discover the host-side ephemeral port Docker picked for our listener.
	port, err := s.engine.PortMapping(ctx, containerInfo.Name, agentproto.ListenPort, "tcp")
	if err != nil {
		return fmt.Errorf("resolve ssh port: %w", err)
	}

	// Pin the workspace's host key into ~/.dcx/known_hosts so plain `ssh`
	// clients can verify it via HostKeyAlias without TOFU.
	_, signer, err := dcxssh.LoadHostKey(ids.WorkspaceID)
	if err != nil {
		return fmt.Errorf("load host key: %w", err)
	}
	if err := dcxssh.PinHostKey(ids.WorkspaceID, signer.PublicKey()); err != nil {
		return fmt.Errorf("pin host key: %w", err)
	}

	knownHosts, _ := dcxssh.KnownHostsPath()

	aliases, err := hostconfig.AddSSHConfig(hostconfig.Entry{
		HostName:       ids.SSHHost,
		ContainerName:  containerInfo.Name,
		WorkspaceID:    ids.WorkspaceID,
		User:           user,
		BindHost:       "127.0.0.1",
		Port:           port,
		KnownHostsPath: knownHosts,
	})
	if err != nil {
		return fmt.Errorf("failed to update SSH config: %w", err)
	}
	if aliases[0] != ids.SSHHost {
		s.report.Warnf("SSH alias %s is used by another workspace; this one is %s", ids.SSHHost, aliases[0])
	}
	s.report.Infof("SSH configured: ssh %s  (127.0.0.1:%d)", aliases[0], port)
	return nil
}

// DownOptions contains options for tearing down a devcontainer.
type DownOptions = container.DownOptions

// DownWithIDs removes the environment using just project name and workspace ID.
func (s *DevContainerService) DownWithIDs(ctx context.Context, projectName, workspaceID string, opts DownOptions) error {
	currentState, containerInfo, err := s.stateManager.GetState(ctx, workspaceID)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if currentState == state.StateAbsent {
		s.report.Infof("No devcontainer found")
		return nil
	}

	// Handle based on plan type (single-container vs compose)
	if containerInfo.IsSingleContainer() {
		if containerInfo.Running {
			if err := s.stopContainers(ctx, containerInfo, projectName); err != nil {
				return err
			}
		}
		// `docker rm -v` only removes the container's anonymous volumes;
		// named volumes are never touched, so this is always safe and avoids
		// leaking anonymous volumes on every recreate.
		if err := s.engine.RemoveContainer(ctx, containerInfo.ID, true, true); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
	} else {
		project := container.ExistingComposeProject(containerInfo, projectName, s.workspacePath)
		if err := project.Down(ctx, s.engine, opts); err != nil {
			return fmt.Errorf("failed to remove environment: %w", err)
		}
		if path, err := container.ComposeOverridePath(containerInfo.WorkspaceID); err == nil && containerInfo.WorkspaceID != "" {
			_ = os.RemoveAll(filepath.Dir(path))
		}
	}

	// Clean up SSH config entry and the per-workspace known_hosts pin so a
	// subsequent `dcx up` with a different host key doesn't produce a
	// "REMOTE HOST IDENTIFICATION HAS CHANGED" scare.
	if containerInfo != nil {
		_ = hostconfig.RemoveSSHConfig(containerInfo.Name)
	}
	if workspaceID != "" {
		_ = dcxssh.RemoveHost(workspaceID)
	}

	s.report.Infof("Devcontainer removed")
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
		ForcePull:       opts.Pull,
		UseLockfile:     !opts.FrozenLockfile, // Don't use lockfile if frozen (verify mode)
		PullForMetadata: true,
	})
	if err != nil {
		return err
	}

	prov, err := container.NewProvisioner(s.engine, s.report, resolved)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	return prov.Build(ctx, container.BuildOptions{
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

	// Verifying needs something to verify against. An empty file (the
	// spec's "initialize me" marker) is not a lockfile either.
	if opts.Mode != LockModeGenerate && existingLockfile == nil {
		if initMarker {
			return nil, fmt.Errorf("lockfile %s is empty: run 'dcx lock' to generate it", lockfile.GetPath(configPath))
		}
		return nil, fmt.Errorf("lockfile not found: run 'dcx lock' to generate one")
	}

	// Create feature manager and resolve features
	configDir := filepath.Dir(configPath)
	mgr, err := features.NewManager(configDir, s.report)
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
	case LockModeVerify, LockModeFrozen:
		if mismatches := features.VerifyLockfile(resolvedFeatures, existingLockfile); len(mismatches) > 0 {
			changes := make([]string, len(mismatches))
			for i, m := range mismatches {
				changes[i] = m.Message
			}
			what := "lockfile verification failed"
			if opts.Mode == LockModeFrozen {
				what = "lockfile mismatch (frozen mode)"
			}
			return nil, fmt.Errorf("%s:\n  - %s", what, strings.Join(changes, "\n  - "))
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
