package service

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/griffithind/dcx/internal/agentctl"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/dcxhome"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/env"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lockfile"
	"github.com/griffithind/dcx/internal/report"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/ssh/hostconfig"
	"github.com/griffithind/dcx/internal/state"
)

// DevContainerService provides high-level operations for devcontainer environments.
type DevContainerService struct {
	idOnce   sync.Once
	identity *devcontainer.Identity

	engine        container.Engine
	report        report.Reporter
	logger        *slog.Logger
	stateManager  *state.StateManager
	workspacePath string
	configPath    string
	verbose       bool

	// Test seams for Up's side effects outside the engine; nil uses the
	// real implementations (agentctl.Provision, lifecycle.HookRunner).
	provisionAgent func(context.Context, container.Engine, report.Reporter, agentctl.Options) error
	newHooks       func(devcontainer.Hooks, int) lifecycleHooks
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
		workspacePath: workspacePath,
		configPath:    configPath,
		verbose:       verbose,
	}
}

// Identity returns the workspace's identity, computed once per service.
// The workspace ID depends only on the path; the name, resource name and
// SSH alias come from devcontainer.json when it can be read, and from the
// directory name otherwise.
func (s *DevContainerService) Identity() *devcontainer.Identity {
	s.idOnce.Do(func() {
		ids := devcontainer.LoadIdentity(s.workspacePath, s.configPath)
		s.identity = &ids
	})
	return s.identity
}

// StateManager returns the state manager for direct access when needed.
func (s *DevContainerService) StateManager() *state.StateManager {
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

	// SSHAccess is who may reach the agent (`dcx up --hosts`). It is fixed
	// when the container is created and recorded on it; nil keeps the
	// recorded access (loopback only for a new container).
	SSHAccess *container.SSHAccess
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
	// RefreshMetadata pulls the base image even when it is local (--pull),
	// so users, hooks and labels come from the image that will run.
	RefreshMetadata bool
}

// Load resolves the devcontainer configuration.
func (s *DevContainerService) Load(ctx context.Context) (*devcontainer.ResolvedDevContainer, error) {
	return s.LoadWithOptions(ctx, LoadOptions{UseLockfile: true})
}

// LoadWithOptions resolves the devcontainer configuration with specified options.
func (s *DevContainerService) LoadWithOptions(ctx context.Context, opts LoadOptions) (*devcontainer.ResolvedDevContainer, error) {
	lock := devcontainer.LockfileIgnore
	if opts.UseLockfile {
		lock = devcontainer.LockfileUse
	}
	return devcontainer.ResolveWorkspace(ctx, devcontainer.ResolveOptions{
		WorkspacePath: s.workspacePath,
		ConfigPath:    s.configPath,
		Images:        engineImages{engine: s.engine, report: s.report, pull: opts.PullForMetadata, refresh: opts.RefreshMetadata},
		Compose:       engineCompose{engine: s.engine},
		Lockfile:      lock,
		ForcePull:     opts.ForcePull,
		Report:        s.report,
	})
}

// engineImages reads image metadata through the container engine, pulling
// the image first when pull is set and it is not available locally, or
// always with refresh.
type engineImages struct {
	engine  container.Engine
	report  report.Reporter
	pull    bool
	refresh bool
}

func (e engineImages) ImageMetadata(ctx context.Context, imageRef string) (string, error) {
	if e.refresh {
		e.report.Infof("Pulling image: %s", imageRef)
		if err := e.engine.PullImageWithProgress(ctx, imageRef, io.Discard); err != nil {
			// A local-only image (built here, or from a registry that is
			// down) keeps the metadata it has.
			e.report.Warnf("Could not pull %s (%v); using the local image's metadata", imageRef, err)
		}
	}
	labels, err := e.engine.ImageLabels(ctx, imageRef)
	if err != nil && e.pull && !e.refresh {
		e.report.Infof("Pulling image: %s", imageRef)
		if perr := e.engine.PullImageWithProgress(ctx, imageRef, io.Discard); perr != nil {
			e.report.Warnf("Could not pull %s to read its metadata: %v", imageRef, perr)
			return "", perr
		}
		labels, err = e.engine.ImageLabels(ctx, imageRef)
	}
	if err != nil {
		return "", err
	}
	return labels[devcontainer.DevcontainerMetadataLabel], nil
}

// engineCompose reads compose models through the container engine.
type engineCompose struct {
	engine container.Engine
}

// ComposeConfig returns the project's model with every profile, so a
// primary service in a profile is found (as devcontainers/cli reads it).
func (e engineCompose) ComposeConfig(ctx context.Context, project, dir string, files []string) ([]byte, error) {
	p := container.ComposeProject{Name: project, Dir: dir, Files: files}
	return e.engine.ComposeOutput(ctx, p, "--profile", "*", "config", "--format", "json")
}

// createEnvironment prepares everything the environment runs (images
// built or pulled), then creates it from that result. On a recreate the
// old environment is removed only after preparing succeeded, so a failing
// build (Dockerfile error, registry outage, broken feature) leaves the
// user with a working container, and nothing is built twice.
func (s *DevContainerService) createEnvironment(ctx context.Context, r *upRun, buildSecrets map[string]string) error {
	prov, err := container.NewProvisioner(s.engine, s.report, r.resolved)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}
	prepared, err := prov.Prepare(ctx, container.UpOptions{
		Rebuild:        r.opts.Rebuild,
		Pull:           r.opts.Pull,
		MetadataPulled: r.opts.Pull,
		BuildSecrets:   buildSecrets,
	})
	if err != nil {
		if r.action == actionRecreate {
			return fmt.Errorf("build failed; the existing devcontainer was left running: %w", err)
		}
		return err
	}

	if r.action == actionRecreate {
		s.report.Infof("Removing existing devcontainer...")
		// Never remove named volumes when recreating: they hold user
		// data that must survive a config change. Only an explicit
		// `dcx down --volumes` removes them. Orphans go: compose runs
		// down with the edited files, so a renamed or removed service
		// (possibly the old primary) would otherwise keep running.
		if err := s.DownWithIDs(ctx, r.ids.ResourceName, r.resolved.WorkspaceID, DownOptions{RemoveOrphans: true}); err != nil {
			return fmt.Errorf("failed to remove existing environment: %w", err)
		}
	}

	if _, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok {
		s.report.Infof("Creating compose-based devcontainer...")
	} else {
		s.report.Infof("Creating single-container devcontainer...")
	}
	// The agent port was free when chosen; another process (or a
	// devcontainer created at the same moment) can take it before Docker
	// binds it.
	return explainPortConflict(prov.Create(ctx, prepared, r.access), prov.PlannedSSHPort())
}

// recordedSSHPort is the agent port recorded on an existing container.
func recordedSSHPort(info *state.ContainerInfo) int {
	if info == nil || info.Labels == nil {
		return 0
	}
	return info.Labels.SSHPort
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
		return explainPortConflict(s.engine.StartContainer(ctx, existing.ID), recordedSSHPort(existing))
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
	return explainPortConflict(prov.Start(ctx), recordedSSHPort(existing))
}

// setupContainerEnvironment applies patches and probes the user environment.
// It returns the probed environment (injected into lifecycle hooks and
// sessions) and remoteEnv with ${containerEnv:…} resolved.
func (s *DevContainerService) setupContainerEnvironment(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, containerInfo *state.ContainerInfo) (probed, remoteEnv map[string]string, err error) {
	patcher := env.NewPatcher(s.engine)
	prober := env.NewProber(s.engine)

	// /etc/environment gets the container's environment (image ENV,
	// features' containerEnv, containerEnv), as devcontainers/cli writes
	// it. remoteEnv is not written there: the file is world-readable and
	// remoteEnv often holds tokens; sessions get it from the agent.
	envToPatch := make(map[string]string)
	for k, v := range resolved.ContainerEnv {
		envToPatch[k] = v
	}
	remoteEnv = resolved.RemoteEnv
	if details, err := s.engine.InspectContainer(ctx, containerInfo.ID); err == nil && details != nil {
		containerEnv := devcontainer.EnvListToMap(details.Env)
		remoteEnv = devcontainer.ResolveContainerEnvRefs(remoteEnv, containerEnv)
		envToPatch = containerEnv
	}

	// Always reconcile dcx's block, so removed variables disappear too.
	if s.verbose {
		s.report.Infof("  [env] Patching /etc/environment with %d variables...", len(envToPatch))
	}
	if skipped, err := patcher.PatchEtcEnvironment(ctx, containerInfo.ID, envToPatch); err != nil {
		s.report.Warnf("Failed to patch /etc/environment: %v", err)
	} else if len(skipped) > 0 {
		s.report.Warnf("Not written to /etc/environment (name or value it cannot express; sessions still get them): %s", strings.Join(skipped, ", "))
	}

	// Patch /etc/profile to preserve PATH from features
	if s.verbose {
		s.report.Infof("  [env] Patching /etc/profile for PATH preservation...")
	}
	if err := patcher.PatchEtcProfile(ctx, containerInfo.ID); err != nil {
		s.report.Warnf("Failed to patch /etc/profile: %v", err)
	}

	// Probe the user environment (loginInteractiveShell unless configured).
	probeType := env.ParseProbeType(resolved.UserEnvProbe)
	if probeType == env.ProbeNone {
		return nil, remoteEnv, nil
	}

	// Probe as the user sessions run as (remoteUser after image metadata
	// is merged), not the raw config's value.
	user := resolved.EffectiveUser

	if s.verbose {
		s.report.Infof("  [env] Probing user environment (mode: %s)...", probeType)
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
// Reads the agent's host port recorded on the container
// (container.SSHHostPort), pins the workspace's host key into
// ~/.dcx/known_hosts, and writes the ~/.ssh/dcx/config block pointing at
// 127.0.0.1:<port>.
func (s *DevContainerService) setupSSHAccess(ctx context.Context, resolved *devcontainer.ResolvedDevContainer, containerInfo *state.ContainerInfo) error {
	if containerInfo == nil {
		_, containerInfo, _ = s.stateManager.State(ctx, resolved.WorkspaceID)
	}
	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Determine user - use EffectiveUser which is already resolved and substituted
	user := resolved.EffectiveUser
	if user == "" {
		user = "root"
	}

	ids := s.Identity()

	port, err := container.SSHHostPort(ctx, s.engine, containerInfo)
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

// portConflict matches Docker's errors for a host port in use ("Bind for
// 127.0.0.1:3000 failed: port is already allocated", "listen tcp4
// 127.0.0.1:3000: bind: address already in use").
var portConflict = regexp.MustCompile(`:(\d+)(?: failed)?: (?:bind: )?(?:port is already allocated|address already in use)`)

// explainPortConflict adds the way out to a create or start that failed
// because a host port the devcontainer publishes is in use. sshPort is the
// agent's port (recorded on an existing container, or just chosen on
// create): only that port changes with a recreate; the user's ports are
// fixed by the config.
func explainPortConflict(err error, sshPort int) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, "port is already allocated") && !strings.Contains(msg, "address already in use") {
		return err
	}
	port := 0
	if m := portConflict.FindStringSubmatch(msg); m != nil {
		port, _ = strconv.Atoi(m[1])
	}
	switch {
	case sshPort > 0 && port == sshPort:
		return fmt.Errorf("%w\nthe SSH port %d is in use by another process; stop it, or run 'dcx up --recreate' to pick a free one", err, port)
	case port > 0:
		return fmt.Errorf("%w\nhost port %d (forwardPorts or appPort) is in use by another process or devcontainer; stop it, or change the port in devcontainer.json", err, port)
	default:
		return fmt.Errorf("%w\na port this devcontainer publishes is in use by another process or devcontainer; stop it, or change the port in devcontainer.json", err)
	}
}

// DownOptions contains options for tearing down a devcontainer.
type DownOptions = container.DownOptions

// Down removes the workspace's environment and its SSH host key, under the
// workspace lock (so it cannot pull the container out from under a running
// up).
func (s *DevContainerService) Down(ctx context.Context, opts DownOptions) error {
	ids := s.Identity()
	unlock, err := s.lockWorkspace(ctx, ids.WorkspaceID)
	if err != nil {
		return err
	}
	defer unlock()
	if err := s.DownWithIDs(ctx, ids.ResourceName, ids.WorkspaceID, opts); err != nil {
		return err
	}
	// A user-requested down retires the workspace's SSH host key too (a
	// recreate inside Up keeps it, so editors' pins stay valid).
	if err := dcxssh.RemoveHostKey(ids.WorkspaceID); err != nil {
		s.report.Warnf("Failed to remove SSH host key: %v", err)
	}
	// The folder keeps only the lock, which is held now; `dcx clean`
	// removes it.
	if path, err := dcxhome.WorkspacePath(ids.WorkspaceID, dcxhome.RecordFile); err == nil {
		_ = os.Remove(path)
	}
	return nil
}

// DownWithIDs removes the environment using just project name and workspace
// ID, without the workspace lock (Up holds it when recreating).
func (s *DevContainerService) DownWithIDs(ctx context.Context, projectName, workspaceID string, opts DownOptions) error {
	currentState, containerInfo, err := s.stateManager.State(ctx, workspaceID)
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
		// `docker rm -v` only removes the container's anonymous volumes, so
		// recreates never lose named ones; `down --volumes` removes those
		// too, as `compose down -v` does for compose environments.
		var volumes []string
		if opts.RemoveVolumes {
			if details, err := s.engine.InspectContainer(ctx, containerInfo.ID); err == nil && details != nil {
				volumes = details.Volumes
			}
		}
		if err := s.engine.RemoveContainer(ctx, containerInfo.ID, true, true); err != nil {
			return fmt.Errorf("failed to remove container: %w", err)
		}
		for _, v := range volumes {
			if err := s.engine.RemoveVolume(ctx, v); err != nil {
				s.report.Warnf("%v", err)
			}
		}
	} else {
		project := container.ExistingComposeProject(containerInfo, projectName, s.workspacePath).ForRemoval(containerInfo.WorkspaceID)
		if err := project.Down(ctx, s.engine, opts); err != nil {
			return fmt.Errorf("failed to remove environment: %w", err)
		}
		if containerInfo.WorkspaceID != "" {
			container.RemoveComposeOverride(containerInfo.WorkspaceID)
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
		UseLockfile:     true,
		PullForMetadata: true,
		RefreshMetadata: opts.Pull,
	})
	if err != nil {
		return err
	}
	if err := s.initLockfile(resolved); err != nil {
		return err
	}
	// The same build inputs as `dcx up`: build secrets included.
	buildSecrets, cleanup, err := s.fetchBuildSecrets(ctx, resolved)
	if err != nil {
		return err
	}
	defer cleanup()

	prov, err := container.NewProvisioner(s.engine, s.report, resolved)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}
	return prov.Build(ctx, container.UpOptions{
		Rebuild:        opts.NoCache,
		NoCache:        opts.NoCache,
		Pull:           opts.Pull,
		MetadataPulled: opts.Pull,
		BuildSecrets:   buildSecrets,
	})
}

// initLockfile writes devcontainer-lock.json when it is an empty file:
// the spec's marker for "create the lockfile on the next build".
func (s *DevContainerService) initLockfile(resolved *devcontainer.ResolvedDevContainer) error {
	if len(resolved.Features) == 0 {
		return nil
	}
	if _, initMarker, err := lockfile.Load(resolved.ConfigPath); err != nil || !initMarker {
		return nil
	}
	if err := features.GenerateLockfile(resolved.Features).Save(resolved.ConfigPath); err != nil {
		return fmt.Errorf("failed to write lockfile: %w", err)
	}
	s.report.Infof("Created %s", lockfile.Path(resolved.ConfigPath))
	return nil
}

// Lock generates, verifies, or checks the devcontainer-lock.json file.
func (s *DevContainerService) Lock(ctx context.Context, opts LockOptions) (*LockResult, error) {
	// Read the config (no resolution yet) for the checks that need no
	// network: anything to lock, and a lockfile to verify against.
	cfg, configPath, err := devcontainer.Load(s.workspacePath, s.configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Check if there are any features to lock
	if len(cfg.Features) == 0 {
		return &LockResult{
			Action:       LockActionNoFeatures,
			LockfilePath: lockfile.Path(configPath),
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
			return nil, fmt.Errorf("lockfile %s is empty: run 'dcx lock' to generate it", lockfile.Path(configPath))
		}
		return nil, fmt.Errorf("lockfile not found: run 'dcx lock' to generate one")
	}

	// Resolve exactly as `dcx up` does; verification resolves through the
	// existing lockfile so it checks what the lockfile pins.
	resolved, err := s.LoadWithOptions(ctx, LoadOptions{UseLockfile: opts.Mode != LockModeGenerate})
	if err != nil {
		return nil, err
	}
	resolvedFeatures := resolved.Features

	// Generate new lockfile from resolved features
	newLockfile := features.GenerateLockfile(resolvedFeatures)
	lockfilePath := lockfile.Path(configPath)

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
