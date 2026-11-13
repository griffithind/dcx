package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lifecycle"
	"github.com/griffithind/dcx/internal/secrets"
	"github.com/griffithind/dcx/internal/ssh/deploy"
	"github.com/griffithind/dcx/internal/state"
)

// lifecycleMarkerDir holds one file per completed once-per-container
// lifecycle phase. It lives in the container's filesystem, so it survives
// stop/start and disappears when the container is recreated.
const lifecycleMarkerDir = "/var/lib/dcx/lifecycle"

// phaseReady is recorded once the first postStartCommand after creation
// succeeded, i.e. setup finished completely.
const phaseReady = "ready"

// upRun carries the state of one Up invocation through the reconcile steps.
type upRun struct {
	opts     UpOptions
	resolved *devcontainer.ResolvedDevContainer
	ids      *Identifiers

	state    state.ContainerState
	existing *state.ContainerInfo // what the state lookup found before any change

	// Set by the steps.
	info      *state.ContainerInfo // the container after create/start
	started   bool                 // the container was created or started by this run
	probedEnv map[string]string
}

// upStep is one idempotent step of bringing an environment up. Each step
// checks what is already in place before acting, so running Up again after
// a failure (or Ctrl-C) continues where the previous run stopped.
type upStep struct {
	name string
	run  func(context.Context, *upRun) error
}

// Up brings a devcontainer environment up: creates, recreates or starts the
// container as needed, then (re)establishes everything a running
// environment needs. It is also the only start path (`dcx up` on a stopped
// container, `dcx restart`).
func (s *DevContainerService) Up(ctx context.Context, opts UpOptions) error {
	resolved, err := s.LoadWithOptions(ctx, LoadOptions{ForcePull: opts.Pull, UseLockfile: true})
	if err != nil {
		return err
	}
	ids, _ := s.GetIdentifiers()

	currentState, existing, err := s.stateManager.GetStateWithProjectAndHash(ctx, ids.ProjectName, resolved.ID, resolved.ConfigHash)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}
	if s.verbose {
		s.report.Infof("Current state: %s", currentState)
	}

	if currentState == state.StateRunning && !opts.Recreate && !opts.Rebuild {
		if s.setupComplete(ctx, existing) {
			s.report.Infof("Devcontainer is already running")
			return nil
		}
		s.report.Infof("The previous setup of this devcontainer did not finish; resuming...")
	} else if resolved.BaseImage != "" {
		// Work to do: reload with the base image pulled if needed, so its
		// devcontainer.metadata applies on the first run too.
		resolved, err = s.LoadWithOptions(ctx, LoadOptions{ForcePull: opts.Pull, UseLockfile: true, PullForMetadata: true})
		if err != nil {
			return err
		}
	}

	if existing.IsLegacyHash() && !opts.Recreate && !opts.Rebuild {
		s.report.Infof("This devcontainer was created by an older dcx, so config changes can't be detected; run 'dcx up --recreate' to apply them.")
	}
	if err := s.checkHostRequirements(ctx, resolved); err != nil {
		return err
	}
	keepComposeProject(resolved, existing)

	run := &upRun{opts: opts, resolved: resolved, ids: ids, state: currentState, existing: existing}
	steps := []upStep{
		{"initialize", s.stepInitialize},
		{"container", s.stepContainer},
		{"agent", s.stepAgent},
		{"secrets", s.stepRuntimeSecrets},
		{"environment", s.stepEnvironment},
		{"lifecycle hooks", s.stepHooks},
		{"ssh config", s.stepSSHConfig},
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("interrupted before %s: %w", step.name, err)
		}
		if err := step.run(ctx, run); err != nil {
			return err
		}
	}
	return nil
}

// checkHostRequirements validates hostRequirements against the Docker host.
func (s *DevContainerService) checkHostRequirements(ctx context.Context, resolved *devcontainer.ResolvedDevContainer) error {
	if resolved.RawConfig == nil || resolved.RawConfig.HostRequirements == nil {
		return nil
	}
	info, err := s.engine.Info(ctx)
	if err != nil {
		s.report.Warnf("Could not get Docker info for resource validation: %v", err)
		return nil
	}
	result := devcontainer.ValidateHostRequirementsWithDocker(resolved.RawConfig.HostRequirements,
		&devcontainer.DockerResources{CPUs: info.NCPU, Memory: info.MemTotal})
	for _, w := range result.Warnings {
		s.report.Warnf("%s", w)
	}
	if !result.Satisfied {
		for _, e := range result.Errors {
			s.report.Errorf("%s", e)
		}
		return fmt.Errorf("host requirements not satisfied")
	}
	return nil
}

// keepComposeProject keeps the compose project an existing environment was
// created with (older dcx versions derived it from "name" alone). Compose
// named volumes are scoped to the project, so recreating under a new name
// would orphan them.
func keepComposeProject(resolved *devcontainer.ResolvedDevContainer, existing *state.ContainerInfo) {
	if existing == nil || existing.ComposeProject == "" {
		return
	}
	if plan, ok := resolved.Plan.(*devcontainer.ComposePlan); ok && plan.ProjectName != existing.ComposeProject {
		kept := *plan
		kept.ProjectName = existing.ComposeProject
		resolved.Plan = &kept
	}
}

// stepInitialize runs initializeCommand on the host. Per the spec it runs
// before the container is created or started, every time Up does work.
func (s *DevContainerService) stepInitialize(ctx context.Context, r *upRun) error {
	return s.hookRunner(r, "").RunInitialize(ctx)
}

// stepContainer makes sure the container exists, matches the config and
// is running.
func (s *DevContainerService) stepContainer(ctx context.Context, r *upRun) error {
	recreate := r.opts.Recreate || r.opts.Rebuild
	switch {
	case r.state == state.StateRunning && !recreate:
		// Resuming an incomplete setup; nothing to do here.
	case r.state == state.StateCreated && !recreate:
		if err := s.startExisting(ctx, r.resolved, r.existing); err != nil {
			return err
		}
		r.started = true
	default:
		buildSecrets, cleanup, err := s.fetchBuildSecrets(ctx, r.resolved)
		if err != nil {
			return err
		}
		defer cleanup()

		if r.state != state.StateAbsent {
			if s.verbose {
				s.report.Infof("Removing existing devcontainer...")
			}
			// Never remove named volumes when recreating: they hold user
			// data that must survive a config change. Only an explicit
			// `dcx down --volumes` removes them.
			if err := s.DownWithIDs(ctx, r.ids.ProjectName, r.resolved.ID, DownOptions{}); err != nil {
				return fmt.Errorf("failed to remove existing environment: %w", err)
			}
		}
		createOpts := r.opts
		createOpts.Rebuild = r.opts.Rebuild || r.state != state.StateAbsent
		if err := s.create(ctx, r.resolved, createOpts, buildSecrets); err != nil {
			return err
		}
		r.started = true
	}

	_, info, err := s.stateManager.GetStateWithProject(ctx, r.ids.ProjectName, r.resolved.ID)
	if err != nil {
		return fmt.Errorf("failed to get container info: %w", err)
	}
	if info == nil {
		return fmt.Errorf("no primary container found after start")
	}
	r.info = info
	return nil
}

// fetchBuildSecrets fetches build secrets into temp files for docker build.
func (s *DevContainerService) fetchBuildSecrets(ctx context.Context, resolved *devcontainer.ResolvedDevContainer) (map[string]string, func(), error) {
	noop := func() {}
	if len(resolved.BuildSecrets) == 0 {
		return nil, noop, nil
	}
	s.report.Infof("Fetching build secrets...")
	fetched, err := secrets.NewFetcher(s.logger).FetchSecrets(ctx, resolved.BuildSecrets)
	if err != nil {
		return nil, noop, fmt.Errorf("failed to fetch build secrets: %w", err)
	}
	paths, cleanup, err := secrets.WriteToTempFiles(fetched, "dcx-build-secret")
	if err != nil {
		return nil, noop, fmt.Errorf("failed to write build secrets: %w", err)
	}
	return paths, cleanup, nil
}

// stepAgent installs the current dcx-agent (replacing an outdated running
// one), mounts its SSH secrets and makes sure it is listening.
func (s *DevContainerService) stepAgent(ctx context.Context, r *upRun) error {
	s.report.Infof("Installing dcx agent...")
	updated, err := deploy.PreDeployAgent(ctx, r.info.Name)
	if err != nil {
		return fmt.Errorf("failed to install dcx agent: %w", err)
	}
	if updated {
		if err := deploy.StopOutdatedAgent(ctx, r.info.Name, agentproto.BinaryPath, agentproto.LoopbackAddr); err != nil {
			return err
		}
	}
	if err := s.mountSSHSecrets(ctx, r.resolved, r.info); err != nil {
		return fmt.Errorf("failed to mount SSH secrets: %w", err)
	}
	if err := s.launchSSHAgent(ctx, r.resolved, r.info, r.opts.SSHAllowedCIDRs); err != nil {
		return fmt.Errorf("failed to launch SSH agent: %w", err)
	}
	return nil
}

// stepRuntimeSecrets fetches and mounts runtime secrets. /run/secrets is a
// tmpfs emptied on every stop, so this runs on every Up that does work.
func (s *DevContainerService) stepRuntimeSecrets(ctx context.Context, r *upRun) error {
	if len(r.resolved.RuntimeSecrets) == 0 {
		return nil
	}
	s.report.Infof("Fetching runtime secrets...")
	fetched, err := secrets.NewFetcher(s.logger).FetchSecrets(ctx, r.resolved.RuntimeSecrets)
	if err != nil {
		return fmt.Errorf("failed to fetch secrets: %w", err)
	}
	s.report.Infof("Mounting secrets...")
	if err := container.MountSecretsToContainer(ctx, s.engine, r.info.Name, fetched, r.resolved.EffectiveUser); err != nil {
		return fmt.Errorf("failed to mount secrets: %w", err)
	}
	return nil
}

// stepEnvironment patches /etc/environment, probes the user environment
// used by lifecycle hooks, and writes the session configuration every SSH
// session gets (environment and postAttachCommand).
func (s *DevContainerService) stepEnvironment(ctx context.Context, r *upRun) error {
	probed, remoteEnv, err := s.setupContainerEnvironment(ctx, r.resolved, r.info)
	if err != nil {
		s.report.Warnf("Environment setup failed: %v", err)
	}
	r.probedEnv = probed
	if err := s.writeSessionConfig(ctx, r, sessionConfig(r.resolved, probed, remoteEnv)); err != nil {
		return fmt.Errorf("write session configuration: %w", err)
	}
	return nil
}

// sessionConfig is what every SSH session gets: the probed user
// environment overlaid with remoteEnv, and postAttachCommand.
func sessionConfig(resolved *devcontainer.ResolvedDevContainer, probed, remoteEnv map[string]string) agentproto.SessionConfig {
	cfg := agentproto.SessionConfig{}
	if len(probed)+len(remoteEnv) > 0 {
		cfg.Env = make(map[string]string, len(probed)+len(remoteEnv))
		for k, v := range probed {
			cfg.Env[k] = v
		}
		for k, v := range remoteEnv {
			cfg.Env[k] = v
		}
	}
	var postAttach interface{}
	if resolved.RawConfig != nil {
		postAttach = resolved.RawConfig.PostAttachCommand
	}
	cfg.PostAttach = lifecycle.AttachSteps(features.CollectPostAttachCommands(resolved.Features), postAttach)
	return cfg
}

// writeSessionConfig writes cfg where the agent reads it for every session.
// It is world-readable because the agent may run as a non-root user; it
// holds nothing a session would not see anyway.
func (s *DevContainerService) writeSessionConfig(ctx context.Context, r *upRun, cfg agentproto.SessionConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := s.engine.WriteFileInContainer(ctx, r.info.Name, agentproto.SessionConfigPath, data, "root"); err != nil {
		return err
	}
	return s.engine.ChmodInContainer(ctx, r.info.Name, agentproto.SessionConfigPath, "0644", "root")
}

// stepHooks runs the once-per-container hooks that have not completed yet,
// then postStartCommand when the container was started by this run or its
// first start never completed.
func (s *DevContainerService) stepHooks(ctx context.Context, r *upRun) error {
	runner := s.hookRunner(r, r.info.ID)
	markers := s.markersFor(r.info)

	if err := runner.RunCreateHooks(ctx, markers); err != nil {
		return fmt.Errorf("lifecycle hooks failed: %w", err)
	}

	firstStartPending := markers != nil && !markers.Done(ctx, phaseReady)
	if r.started || firstStartPending {
		if err := runner.RunStartHooks(ctx); err != nil {
			return fmt.Errorf("lifecycle hooks failed: %w", err)
		}
	}
	if markers != nil && firstStartPending {
		if err := markers.Mark(ctx, phaseReady); err != nil {
			return fmt.Errorf("record setup completion: %w", err)
		}
	}
	return nil
}

// stepSSHConfig writes the ~/.ssh/config entry. Failure is not fatal.
func (s *DevContainerService) stepSSHConfig(ctx context.Context, r *upRun) error {
	if err := s.setupSSHAccess(ctx, r.resolved, r.info); err != nil {
		s.report.Warnf("Failed to setup SSH access: %v", err)
	}
	return nil
}

// hookRunner builds a lifecycle hook runner for the resolved config.
func (s *DevContainerService) hookRunner(r *upRun, containerID string) *lifecycle.HookRunner {
	runner := lifecycle.NewHookRunner(s.engine, s.report, containerID, s.workspacePath, r.resolved.RawConfig, r.resolved.ID)
	if r.probedEnv != nil {
		runner.SetProbedEnv(r.probedEnv)
	}
	if len(r.resolved.Features) > 0 {
		runner.SetFeatureHooks(
			features.CollectOnCreateCommands(r.resolved.Features),
			features.CollectUpdateContentCommands(r.resolved.Features),
			features.CollectPostCreateCommands(r.resolved.Features),
			features.CollectPostStartCommands(r.resolved.Features),
		)
	}
	return runner
}

// setupComplete reports whether a running container finished its setup.
// Containers created before lifecycle markers existed are assumed complete.
func (s *DevContainerService) setupComplete(ctx context.Context, info *state.ContainerInfo) bool {
	m := s.markersFor(info)
	return m == nil || m.Done(ctx, phaseReady)
}

// markersFor returns marker storage for a container, or nil when the
// container does not track lifecycle progress (created by older dcx).
func (s *DevContainerService) markersFor(info *state.ContainerInfo) *containerMarkers {
	if info == nil || info.Labels == nil || !info.Labels.LifecycleMarkers {
		return nil
	}
	return &containerMarkers{engine: s.engine, container: info.ID}
}

// containerMarkers stores lifecycle markers as files in the container.
type containerMarkers struct {
	engine    container.Engine
	container string
}

var _ lifecycle.Markers = (*containerMarkers)(nil)

func markerPath(phase string) string {
	return path.Join(lifecycleMarkerDir, strings.ReplaceAll(phase, "/", "_")+".done")
}

func (m *containerMarkers) Done(ctx context.Context, phase string) bool {
	return m.engine.ExecInContainer(ctx, m.container, []string{"test", "-f", markerPath(phase)}) == nil
}

func (m *containerMarkers) Mark(ctx context.Context, phase string) error {
	if err := m.engine.MkdirInContainer(ctx, m.container, lifecycleMarkerDir, "root"); err != nil {
		return err
	}
	stamp := []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
	return m.engine.WriteFileInContainer(ctx, m.container, markerPath(phase), stamp, "root")
}
