package service

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/agentctl"
	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/dcxhome"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/lifecycle"
	"github.com/griffithind/dcx/internal/secrets"
	"github.com/griffithind/dcx/internal/ssh/hostconfig"
	"github.com/griffithind/dcx/internal/state"
)

// lifecycleMarkerDir holds one file per completed once-per-container
// lifecycle phase. It lives in the container's filesystem, so it survives
// stop/start and disappears when the container is recreated.
const lifecycleMarkerDir = "/var/lib/dcx/lifecycle"

// phaseReady is recorded once the first postStartCommand after creation
// succeeded, i.e. setup finished completely.
const phaseReady = "ready"

// phasePostStart is rewritten (with the time) after every postStartCommand,
// so a start outside dcx can be told from an agent that merely died.
const phasePostStart = "poststart"

// upRun carries the state of one Up invocation through the reconcile steps.
type upRun struct {
	opts     UpOptions
	resolved *devcontainer.ResolvedDevContainer
	ids      *devcontainer.Identity

	action   upAction
	access   container.SSHAccess // SSH access of the container being created
	state    state.ContainerState
	existing *state.ContainerInfo // what the state lookup found before any change

	// Set by the steps.
	info    *state.ContainerInfo // the container after create/start
	started bool                 // the container was created or started by this run
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
// container, `dcx restart`). It reports whether there was anything to do.
func (s *DevContainerService) Up(ctx context.Context, opts UpOptions) (bool, error) {
	// Decide and act under the workspace lock: a concurrent Up would see
	// the same state and both would create (or recreate) the container,
	// and a concurrent down could remove it mid-setup.
	unlock, err := s.lockWorkspace(ctx, s.Identity().WorkspaceID)
	if err != nil {
		return false, err
	}
	defer unlock()
	return s.up(ctx, opts)
}

// planUp resolves the configuration, looks at what exists and decides what
// Up does. It returns nil when there is nothing to do.
func (s *DevContainerService) planUp(ctx context.Context, opts UpOptions) (*upRun, error) {
	// One resolution: the base image is pulled for its devcontainer.metadata
	// only when it is not present (the first run), so its metadata applies
	// then too. (It used to resolve twice, features included.)
	resolved, err := s.LoadWithOptions(ctx, LoadOptions{ForcePull: opts.Pull, UseLockfile: true, PullForMetadata: true, RefreshMetadata: opts.Pull})
	if err != nil {
		return nil, err
	}
	if err := s.initLockfile(resolved); err != nil {
		return nil, err
	}
	// The record says which workspace the folder in $DCX_HOME belongs to.
	if err := dcxhome.WriteWorkspace(resolved.WorkspaceID, dcxhome.Workspace{
		Path: s.workspacePath, Config: resolved.ConfigPath, Name: resolved.Name,
	}); err != nil {
		s.report.Warnf("Could not write the workspace record: %v", err)
	}
	ids := &resolved.Identity

	currentState, existing, err := s.stateManager.StateWithHash(ctx, resolved.WorkspaceID, resolved.ConfigHash)
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %w", err)
	}
	if s.verbose {
		s.report.Infof("Current state: %s", currentState)
	}

	// A running container counts as ready only when its setup finished and
	// its agent answers: after a restart outside dcx (docker restart, a
	// reboot with a restart policy) the markers survive but the agent and
	// the /run/secrets tmpfs do not.
	setupDone, agentUp := false, false
	if currentState == state.StateRunning {
		if setupDone, err = s.setupComplete(ctx, existing); err != nil {
			return nil, err
		}
		agentUp = setupDone && agentctl.Listening(ctx, s.engine, existing.Name, container.SSHListenAddr(existing))
	}
	action := decideUp(currentState, setupDone && agentUp, opts)
	access, err := sshAccessFor(action, existing, opts.SSHAccess)
	if err != nil {
		return nil, err
	}
	s.announceUp(ctx, action, setupDone, existing, resolved, opts)
	if action == actionNone {
		return &upRun{opts: opts, resolved: resolved, ids: ids, action: action, state: currentState, existing: existing}, nil
	}
	s.checkHostRequirements(ctx, resolved)
	keepComposeProject(resolved, existing)

	run := &upRun{opts: opts, resolved: resolved, ids: ids, action: action, access: access, state: currentState, existing: existing}
	// A container whose agent is gone after its setup completed was usually
	// started outside dcx (docker restart, a reboot with a restart policy):
	// postStartCommand runs when it started after the last one ran.
	if action == actionResume && setupDone {
		run.started = s.startedSinceLastPostStart(ctx, existing)
	}
	return run, nil
}

// up is Up without taking the workspace lock (the caller holds it). It
// reports whether there was anything to do (false: the environment was
// already up and set up).
func (s *DevContainerService) up(ctx context.Context, opts UpOptions) (bool, error) {
	// initializeCommand runs on every up, as in devcontainers/cli, even
	// when the container is already up and set up.
	if err := s.runInitialize(ctx); err != nil {
		return false, err
	}
	run, err := s.planUp(ctx, opts)
	if err != nil || run == nil || run.action == actionNone {
		return false, err
	}
	steps := []upStep{
		{"container", s.stepContainer},
		{"agent", s.stepAgent},
		{"secrets", s.stepRuntimeSecrets},
		{"environment", s.stepEnvironment},
		// SSH access before the hooks: when a hook fails, the user can
		// still get in to see why.
		{"ssh config", s.stepSSHConfig},
		{"lifecycle hooks", s.stepHooks},
	}
	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			return true, fmt.Errorf("interrupted before %s: %w", step.name, err)
		}
		if err := step.run(ctx, run); err != nil {
			return true, err
		}
	}
	return true, nil
}

// reportPreviousIDContainer points at a container this config created
// under an older dcx: a config outside the default locations used to share
// the folder's workspace ID, so that container is not found under the
// config's own ID and a new environment is created next to it.
func (s *DevContainerService) reportPreviousIDContainer(ctx context.Context, resolved *devcontainer.ResolvedDevContainer) {
	folderID := devcontainer.ComputeID(resolved.LocalRoot)
	if folderID == resolved.WorkspaceID {
		return
	}
	old, err := s.engine.ListContainersWithLabels(ctx, map[string]string{
		state.LabelWorkspaceID: folderID,
		state.LabelConfigPath:  resolved.ConfigPath,
	})
	if err != nil || len(old) == 0 {
		return
	}
	remove := "docker rm -f " + old[0].Name
	if project := state.ContainerLabelsFromMap(old[0].Labels).ComposeProject; project != "" {
		remove = "docker compose -p " + project + " down"
	}
	s.report.Warnf("Container %s was created for this config by an older dcx (under the folder's ID) and is left as is; remove it with '%s' once the new environment works.", old[0].Name, remove)
}

// inputsDrifted reports whether what determined the container changed
// while the files the user edits did not (see ComputeInputsHash): dcx
// reports that instead of recreating, since such inputs change without
// anyone editing anything (a new SSH agent socket, a moved feature tag).
func inputsDrifted(existing *state.ContainerInfo, resolved *devcontainer.ResolvedDevContainer) bool {
	if existing == nil || existing.Labels == nil || existing.IsLegacyHash() {
		return false
	}
	recorded := existing.Labels.HashInputs
	return recorded != "" && existing.ConfigHash == resolved.ConfigHash && recorded != resolved.InputsHash
}

// announceUp tells the user what up is about to do and why (drift, a
// reconnect, a resumed setup, an older container, nothing to do).
func (s *DevContainerService) announceUp(ctx context.Context, action upAction, setupDone bool, existing *state.ContainerInfo, resolved *devcontainer.ResolvedDevContainer, opts UpOptions) {
	if (action == actionNone || action == actionStart || action == actionResume) && inputsDrifted(existing, resolved) {
		s.report.Infof("Some inputs changed since this devcontainer was created (a substituted value such as ${localEnv:...}, or a feature tag that now points to a new release); it was kept. Run 'dcx up --recreate' to apply them.")
	}
	switch {
	case action == actionNone:
		s.report.Infof("Devcontainer is already running")
	case action == actionResume && setupDone:
		s.report.Infof("The dcx agent is not running (was the container restarted outside dcx?); reconnecting...")
	case action == actionResume:
		s.report.Infof("The previous setup of this devcontainer did not finish; resuming...")
	case action == actionCreate:
		s.reportPreviousIDContainer(ctx, resolved)
	}
	if existing.IsLegacyHash() && !opts.Recreate && !opts.Rebuild {
		s.report.Infof("This devcontainer was created by an older dcx, so config changes can't be detected; run 'dcx up --recreate' to apply them.")
	}
}

// upAction is what Up does with the workspace's container.
type upAction int

const (
	actionNone     upAction = iota // running and fully set up
	actionResume                   // running, but the last setup did not finish
	actionStart                    // exists, not running
	actionCreate                   // no container yet
	actionRecreate                 // remove and create again (stale, broken, or forced)
)

// decideUp is the single place that decides what `dcx up` does, from the
// container's state, whether its setup completed, and the --recreate /
// --rebuild flags (which always win).
func decideUp(st state.ContainerState, setupComplete bool, opts UpOptions) upAction {
	forced := opts.Recreate || opts.Rebuild
	switch st {
	case state.StateAbsent, state.StateUnknown:
		return actionCreate
	case state.StateRunning:
		switch {
		case forced:
			return actionRecreate
		case setupComplete:
			return actionNone
		default:
			return actionResume
		}
	case state.StateCreated:
		if forced {
			return actionRecreate
		}
		return actionStart
	default: // stale, broken
		return actionRecreate
	}
}

// sshAccessFor returns the SSH access of the container Up ends up with.
// Access is fixed when a container is created (Docker publishes ports only
// then), so it is recorded on the container and kept by later runs,
// restarts and recreates; --hosts changes it only together with a
// (re)create.
func sshAccessFor(action upAction, existing *state.ContainerInfo, requested *container.SSHAccess) (container.SSHAccess, error) {
	recorded := container.RecordedSSHAccess(existing)
	switch {
	case requested == nil:
		return recorded, nil
	case action == actionCreate || action == actionRecreate:
		return *requested, nil
	case !requested.Equal(recorded):
		return recorded, fmt.Errorf("--hosts=%s differs from this devcontainer's SSH access (%s), which is fixed when it is created; "+
			"run 'dcx up --recreate --hosts=%s' to change it", requested, recorded, requested)
	default:
		return recorded, nil
	}
}

// checkHostRequirements reports hostRequirements the Docker host falls
// short of, and drops the GPU request when the host has no GPU runtime
// (`--gpus` would fail the start), as devcontainers/cli does.
func (s *DevContainerService) checkHostRequirements(ctx context.Context, resolved *devcontainer.ResolvedDevContainer) {
	if resolved.HostRequirements == nil {
		return
	}
	info, err := s.engine.Info(ctx)
	if err != nil {
		s.report.Warnf("Could not get Docker info for resource validation: %v", err)
		return
	}
	if !info.GPU {
		resolved.GPU = nil
	}
	for _, w := range devcontainer.HostRequirementWarnings(resolved.HostRequirements,
		&devcontainer.DockerResources{CPUs: info.NCPU, Memory: info.MemTotal, GPU: info.GPU}) {
		s.report.Warnf("%s", w)
	}
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

// runInitialize runs initializeCommand on the host before anything is
// resolved, as devcontainers/cli does: it may generate what resolution
// reads (compose files, a Dockerfile, an .env file). It comes only from
// devcontainer.json, so only that file is read for it.
func (s *DevContainerService) runInitialize(ctx context.Context) error {
	local, _, err := devcontainer.Load(s.workspacePath, s.configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	return s.hookRunner(devcontainer.Hooks{Initialize: devcontainer.InitializeHooks(local)}, "").RunInitialize(ctx)
}

// stepContainer makes sure the container exists, matches the config and
// is running.
func (s *DevContainerService) stepContainer(ctx context.Context, r *upRun) error {
	switch r.action {
	case actionResume:
		// The container is running; only the setup steps remain.
	case actionStart:
		if err := s.startExisting(ctx, r.resolved, r.existing); err != nil {
			return err
		}
		r.started = true
	case actionCreate, actionRecreate:
		buildSecrets, cleanup, err := s.fetchBuildSecrets(ctx, r.resolved)
		if err != nil {
			return err
		}
		defer cleanup()

		if err := s.createEnvironment(ctx, r, buildSecrets); err != nil {
			return err
		}
		r.started = true
	}

	_, info, err := s.stateManager.State(ctx, r.resolved.WorkspaceID)
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

// stepAgent installs (or upgrades) the dcx-agent, writes its SSH secrets
// and makes sure it is listening.
func (s *DevContainerService) stepAgent(ctx context.Context, r *upRun) error {
	provision := agentctl.Provision
	if s.provisionAgent != nil {
		provision = s.provisionAgent
	}
	return provision(ctx, s.engine, s.report, agentctl.Options{
		ContainerName: r.info.Name,
		WorkspaceID:   r.ids.WorkspaceID,
		User:          r.resolved.EffectiveUser,
		WorkDir:       r.resolved.WorkspaceFolder,
		ListenAddr:    container.SSHListenAddr(r.info),
		AllowedCIDRs:  container.RecordedSSHAccess(r.info).AllowedCIDRs,
	})
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
	cfg.PostAttach = lifecycle.AttachSteps(resolved.Hooks.PostAttach)
	cfg.Unset = resolved.RemoteEnvUnset
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
	alias := ""
	if aliases := hostconfig.Aliases(r.info.Name); len(aliases) > 0 {
		alias = aliases[0]
	}
	runner := s.hookRunner(r.resolved.Hooks, alias)
	markers := s.markersFor(r.info)

	var created lifecycle.Markers // a nil *containerMarkers must stay a nil interface
	if markers != nil {
		created = markers
	}
	if err := runner.RunCreateHooks(ctx, created); err != nil {
		return fmt.Errorf("lifecycle hooks failed: %w", err)
	}

	firstStartPending := false
	if markers != nil {
		ready, err := markers.Done(ctx, phaseReady)
		if err != nil {
			return fmt.Errorf("cannot tell whether setup completed: %w", err)
		}
		firstStartPending = !ready
	}
	if r.started || firstStartPending {
		if err := runner.RunStartHooks(ctx); err != nil {
			return fmt.Errorf("lifecycle hooks failed: %w", err)
		}
		if markers != nil {
			if err := markers.Mark(ctx, phasePostStart); err != nil {
				return fmt.Errorf("record postStartCommand: %w", err)
			}
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

// lifecycleHooks runs the lifecycle phases (*lifecycle.HookRunner; tests
// substitute it through DevContainerService.newHooks).
type lifecycleHooks interface {
	RunInitialize(ctx context.Context) error
	RunCreateHooks(ctx context.Context, markers lifecycle.Markers) error
	RunStartHooks(ctx context.Context) error
}

// hookRunner returns a runner for hooks; container hooks run with OpenSSH
// through sshAlias (written by the ssh config step).
func (s *DevContainerService) hookRunner(hooks devcontainer.Hooks, sshAlias string) lifecycleHooks {
	if s.newHooks != nil {
		return s.newHooks(hooks, sshAlias)
	}
	return lifecycle.NewHookRunner(sshAlias, s.report, s.workspacePath, hooks)
}

// startedSinceLastPostStart reports whether the container started after
// postStartCommand last ran (the poststart marker, else the ready marker).
// When that can't be told it answers true: re-running postStart after a
// reconnect is the lesser evil next to skipping it after a restart.
func (s *DevContainerService) startedSinceLastPostStart(ctx context.Context, info *state.ContainerInfo) bool {
	details, err := s.engine.InspectContainer(ctx, info.ID)
	if err != nil || details == nil {
		return true
	}
	started, err := time.Parse(time.RFC3339Nano, details.StartedAt)
	if err != nil {
		return true
	}
	for _, phase := range []string{phasePostStart, phaseReady} {
		out, err := s.engine.ExecOutput(ctx, info.ID, []string{"cat", markerPath(phase)})
		if err != nil {
			continue
		}
		if ran, err := time.Parse(time.RFC3339, strings.TrimSpace(string(out))); err == nil {
			return started.After(ran)
		}
	}
	return true
}

// setupComplete reports whether a running container finished its setup.
// Containers created before lifecycle markers existed are assumed complete.
func (s *DevContainerService) setupComplete(ctx context.Context, info *state.ContainerInfo) (bool, error) {
	m := s.markersFor(info)
	if m == nil {
		return true, nil
	}
	done, err := m.Done(ctx, phaseReady)
	if err != nil {
		return false, fmt.Errorf("cannot tell whether setup completed: %w", err)
	}
	return done, nil
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

// markerQuery prints "done" when the marker file ($1) exists.
const markerQuery = `if [ -f "$1" ]; then echo done; fi`

func (m *containerMarkers) Done(ctx context.Context, phase string) (bool, error) {
	out, err := m.engine.ExecOutput(ctx, m.container, []string{"sh", "-c", markerQuery, "dcx-marker", markerPath(phase)})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(string(out)) == "done", nil
}

func (m *containerMarkers) Mark(ctx context.Context, phase string) error {
	if err := m.engine.MkdirInContainer(ctx, m.container, lifecycleMarkerDir, "root"); err != nil {
		return err
	}
	stamp := []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
	return m.engine.WriteFileInContainer(ctx, m.container, markerPath(phase), stamp, "root")
}
