package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/report"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/griffithind/dcx/internal/build"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
)

// UnifiedRuntime creates and starts the environment for all plan types.
// It handles image-based, Dockerfile-based, and compose-based devcontainers
// through a single unified implementation.
type UnifiedRuntime struct {
	engine   Engine
	report   report.Reporter
	resolved *devcontainer.ResolvedDevContainer
	builder  *build.Builder

	containerID   string
	containerName string

	derivedImage string
	sshBindHost  string // "" == 127.0.0.1 (the default)
}

// NewUnifiedRuntime creates a new runtime for a resolved devcontainer.
func NewUnifiedRuntime(engine Engine, rep report.Reporter, resolved *devcontainer.ResolvedDevContainer) (*UnifiedRuntime, error) {
	if resolved == nil {
		return nil, fmt.Errorf("resolved devcontainer is required")
	}

	rep = report.OrDiscard(rep)
	builder := build.NewBuilder(engine, rep)

	return &UnifiedRuntime{
		engine:        engine,
		report:        rep,
		resolved:      resolved,
		builder:       builder,
		containerName: resolved.ServiceName,
	}, nil
}

// Up implements ContainerRuntime.Up.
func (r *UnifiedRuntime) Up(ctx context.Context, opts UpOptions) error {
	if r.resolved == nil {
		return fmt.Errorf("no resolved configuration - use NewUnifiedRuntime")
	}

	r.sshBindHost = opts.SSHBindHost

	hasFeatures := len(r.resolved.Features) > 0

	// Determine the approach based on plan type
	switch plan := r.resolved.Plan.(type) {
	case *devcontainer.ComposePlan:
		return r.upCompose(ctx, opts, hasFeatures, plan)
	case *devcontainer.ImagePlan, *devcontainer.DockerfilePlan:
		return r.upSingle(ctx, opts)
	default:
		return fmt.Errorf("unsupported plan type: %T", plan)
	}
}

// upCompose handles compose-based configurations.
func (r *UnifiedRuntime) upCompose(ctx context.Context, opts UpOptions, hasFeatures bool, plan *devcontainer.ComposePlan) error {
	image, err := r.buildImages(ctx, opts)
	if err != nil {
		return err
	}

	if err := r.writeComposeOverride(plan, r.spec(image)); err != nil {
		return fmt.Errorf("failed to write compose override: %w", err)
	}
	project := r.composeProject(plan)

	// Build secrets only matter when compose itself builds the service
	// (no dcx layers). They live in a one-off override so the persistent
	// one never references deleted secret files.
	if len(opts.BuildSecrets) > 0 && r.derivedImage == "" {
		path, err := writeTempOverride(plan.Service, ContainerSpec{}, opts.BuildSecrets)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(path) }()
		project = project.with(path)
	}

	args := []string{"up", "-d"}
	// Add --build only if explicitly requested and we DON'T have features
	if opts.Build && !hasFeatures {
		args = append(args, "--build")
	}
	args = append(args, composeServices(plan)...)
	return r.engine.Compose(ctx, project, args...)
}

// composeServices returns the services `up` starts: all of them when
// runServices is empty, otherwise runServices plus the primary service,
// which the spec says is always started.
func composeServices(plan *devcontainer.ComposePlan) []string {
	if len(plan.RunServices) == 0 {
		return nil
	}
	services := append([]string(nil), plan.RunServices...)
	for _, s := range services {
		if s == plan.Service {
			return services
		}
	}
	return append(services, plan.Service)
}

// upSingle handles single-container configurations (image or Dockerfile).
func (r *UnifiedRuntime) upSingle(ctx context.Context, opts UpOptions) error {
	finalImage, err := r.buildImages(ctx, opts)
	if err != nil {
		return err
	}

	// Create the container
	containerID, err := r.createContainer(ctx, finalImage)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Start the container
	if err := r.engine.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	r.containerID = containerID
	return nil
}

// buildImages builds the image the dev container runs and returns it.
//
// The base image is pulled or built (the Dockerfile / compose build always
// runs; BuildKit's cache makes that cheap and picks up context changes),
// then dcx's feature and UID layers are added (see layerImages). For a
// compose plan without features or a UID update there is nothing to layer:
// compose builds and runs the service image itself, and "" is returned.
//
// Up and Build both use this, so `dcx build` produces exactly the image
// `dcx up` would run.
func (r *UnifiedRuntime) buildImages(ctx context.Context, opts UpOptions) (string, error) {
	if plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok {
		if len(r.resolved.Features) == 0 && !r.resolved.ShouldUpdateUID {
			return "", nil
		}
		if err := r.ensureServicesBuilt(ctx, plan, opts.BuildSecrets); err != nil {
			return "", fmt.Errorf("failed to build services: %w", err)
		}
		baseImage, err := r.getComposeBaseImage(ctx, plan)
		if err != nil {
			return "", fmt.Errorf("failed to determine base image: %w", err)
		}
		finalImage, err := r.layerImages(ctx, baseImage, opts.Rebuild)
		if err != nil {
			return "", err
		}
		if finalImage != baseImage {
			r.derivedImage = finalImage
		}
		return finalImage, nil
	}

	baseImage, err := r.resolveBaseImage(ctx, opts)
	if err != nil {
		return "", err
	}
	finalImage, err := r.layerImages(ctx, baseImage, opts.Rebuild)
	if err != nil {
		return "", err
	}
	if finalImage != baseImage {
		r.derivedImage = finalImage
	}
	return finalImage, nil
}

// resolveBaseImage determines the base image for single-container configs.
func (r *UnifiedRuntime) resolveBaseImage(ctx context.Context, opts UpOptions) (string, error) {
	switch plan := r.resolved.Plan.(type) {
	case *devcontainer.ImagePlan:
		r.report.Infof("Using image: %s", plan.Image)

		exists, err := r.engine.ImageExists(ctx, plan.Image)
		if err != nil {
			return "", fmt.Errorf("failed to check image: %w", err)
		}

		if !exists || opts.Pull {
			r.report.Infof("Pulling image: %s", plan.Image)
			if err := r.engine.PullImageWithProgress(ctx, plan.Image, os.Stdout); err != nil {
				return "", fmt.Errorf("failed to pull image: %w", err)
			}
		}

		return plan.Image, nil

	case *devcontainer.DockerfilePlan:
		imageTag := fmt.Sprintf("%s%s:base", ImageTagPrefix, r.resolved.ID)
		r.report.Infof("Building image: %s", imageTag)

		if err := r.buildDockerfile(ctx, imageTag, plan, opts.BuildSecrets); err != nil {
			return "", fmt.Errorf("failed to build image: %w", err)
		}

		return imageTag, nil
	}

	return "", fmt.Errorf("no image or build configuration found")
}

// buildDockerfile builds an image from a Dockerfile using the CLI.
func (r *UnifiedRuntime) buildDockerfile(ctx context.Context, imageTag string, plan *devcontainer.DockerfilePlan, buildSecrets map[string]string) error {
	buildCtx := plan.Context
	if buildCtx == "" {
		buildCtx = r.resolved.ConfigDir
	}

	// Resolve relative paths
	if !filepath.IsAbs(buildCtx) {
		buildCtx = filepath.Join(r.resolved.ConfigDir, buildCtx)
	}

	dockerfilePath := plan.Dockerfile
	if !filepath.IsAbs(dockerfilePath) {
		dockerfilePath = filepath.Join(r.resolved.ConfigDir, dockerfilePath)
	}

	buildArgs := make(map[string]string)
	for k, v := range plan.Args {
		buildArgs[k] = v
	}

	// Generate metadata for the built image (local config only, no base or features yet)
	metadata, _ := build.GenerateMetadataLabel("", nil, r.resolved.RawConfig)

	_, err := r.builder.BuildFromDockerfile(ctx, build.DockerfileBuildOptions{
		Tag:        imageTag,
		Dockerfile: dockerfilePath,
		Context:    buildCtx,
		Args:       buildArgs,
		Target:     plan.Target,
		Progress:   os.Stdout,
		Metadata:   metadata,
		Secrets:    buildSecrets,
		Options:    plan.Options,
	})
	return err
}

// layerImages builds (or reuses) the image layers dcx adds on top of the
// base image: features first, then the UID update. Returns the final image.
//
// Each layer is tagged dcx/<workspace>:<kind>-<hash>, where the hash covers
// the ID of the image it is built on plus the layer's own inputs. A new base
// (pulled or rebuilt) therefore produces new tags instead of reusing stale
// layers, and a cached features image still gets its UID layer.
func (r *UnifiedRuntime) layerImages(ctx context.Context, baseImage string, rebuild bool) (string, error) {
	image := baseImage

	if len(r.resolved.Features) > 0 {
		baseID, err := r.engine.GetImageID(ctx, image)
		if err != nil {
			return "", fmt.Errorf("failed to inspect base image %s: %w", image, err)
		}
		tag := r.layerTag("features", baseID, devcontainer.FeaturesKey(r.resolved.Features),
			r.resolved.RemoteUser, r.resolved.ContainerUser)

		baseImageMetadata := ""
		if labels, err := r.engine.GetImageLabels(ctx, image); err == nil {
			baseImageMetadata = labels[devcontainer.DevcontainerMetadataLabel]
		}
		image, err = r.builder.BuildWithFeatures(ctx, build.FeatureBuildOptions{
			BaseImage:         image,
			Tag:               tag,
			Features:          r.resolved.Features,
			RemoteUser:        r.resolved.RemoteUser,
			ContainerUser:     r.resolved.ContainerUser,
			Rebuild:           rebuild,
			Progress:          os.Stdout,
			BaseImageMetadata: baseImageMetadata,
			LocalConfig:       r.resolved.RawConfig,
		})
		if err != nil {
			return "", fmt.Errorf("failed to build derived image with features: %w", err)
		}
	}

	if r.resolved.ShouldUpdateUID {
		imageID, err := r.engine.GetImageID(ctx, image)
		if err != nil {
			return "", fmt.Errorf("failed to inspect image %s: %w", image, err)
		}
		effectiveUser := r.resolved.EffectiveUser
		imageUser := r.resolved.ContainerUser
		if imageUser == "" {
			imageUser = effectiveUser
		}
		hostUID, hostGID := r.resolved.HostUID, r.resolved.HostGID
		tag := r.layerTag("uid", imageID, effectiveUser, imageUser, strconv.Itoa(hostUID), strconv.Itoa(hostGID))

		image, err = r.builder.BuildUIDUpdate(ctx, build.UIDBuildOptions{
			BaseImage:  image,
			Tag:        tag,
			RemoteUser: effectiveUser,
			ImageUser:  imageUser,
			HostUID:    hostUID,
			HostGID:    hostGID,
			Rebuild:    rebuild,
			Progress:   os.Stdout,
		})
		if err != nil {
			return "", fmt.Errorf("failed to build UID update image: %w", err)
		}
	}

	return image, nil
}

// layerTag returns dcx/<workspaceID>:<kind>-<hash of parts>.
func (r *UnifiedRuntime) layerTag(kind string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%s%s:%s-%s", ImageTagPrefix, r.resolved.ID, kind,
		hex.EncodeToString(sum[:])[:hashTruncationLength])
}

// createContainer creates the primary container from imageRef.
func (r *UnifiedRuntime) createContainer(ctx context.Context, imageRef string) (string, error) {
	return r.engine.CreateContainer(ctx, r.spec(imageRef))
}

// spec builds the ContainerSpec of the primary container. It is the single
// place runtime settings are derived from the resolved configuration, for
// both single-container and compose plans.
//
// Settings that only exist for `docker run` are left out for compose: the
// container name and working directory (compose owns them), the workspace
// mount (the compose file defines it, per the spec) and runArgs.
func (r *UnifiedRuntime) spec(image string) ContainerSpec {
	res := r.resolved
	_, isCompose := res.Plan.(*devcontainer.ComposePlan)

	s := ContainerSpec{
		Image:       image,
		User:        res.ContainerUser,
		Labels:      r.buildLabels(),
		Env:         r.buildEnvironment(),
		Ports:       r.buildPortBindings(),
		CapAdd:      append([]string(nil), res.CapAdd...),
		SecurityOpt: append([]string(nil), res.SecurityOpt...),
		Privileged:  res.Privileged,
		Init:        res.Init,
	}
	s.Mounts, s.Tmpfs = r.buildMounts()

	if !isCompose {
		s.Name = res.ServiceName
		s.WorkingDir = res.WorkspaceFolder
		if wm := r.workspaceMount(); wm != nil {
			s.Mounts = append([]devcontainer.Mount{*wm}, s.Mounts...)
		}
		r.applyRunArgs(&s)
	}

	r.applyFeatureRequirements(&s)

	if gpu := res.GPURequirements; gpu != nil && gpu.Enabled {
		if gpu.Count > 0 {
			s.GPUs = strconv.Itoa(gpu.Count)
		} else {
			s.GPUs = "all"
		}
	}

	if r.overrideCommand() {
		s.Entrypoint = append([]string(nil), keepAliveEntrypoint...)
	}
	return s
}

// workspaceMount returns the workspace mount of a single-container plan:
// workspaceMount from the config, or a bind of the local workspace.
func (r *UnifiedRuntime) workspaceMount() *devcontainer.Mount {
	if r.resolved.WorkspaceMount != "" {
		if m := devcontainer.ParseWorkspaceMount(r.resolved.WorkspaceMount); m != nil {
			return m
		}
	}
	if r.resolved.LocalRoot != "" && r.resolved.WorkspaceFolder != "" {
		return &devcontainer.Mount{Type: "bind", Source: r.resolved.LocalRoot, Target: r.resolved.WorkspaceFolder}
	}
	return nil
}

// applyRunArgs applies the runArgs dcx models to s and keeps the rest for
// verbatim pass-through.
func (r *UnifiedRuntime) applyRunArgs(s *ContainerSpec) {
	ra := r.resolved.RunArgs
	if ra == nil {
		return
	}
	if ra.NetworkMode != "" {
		s.NetworkMode = ra.NetworkMode
	}
	if ra.IpcMode != "" {
		s.IpcMode = ra.IpcMode
	}
	if ra.PidMode != "" {
		s.PidMode = ra.PidMode
	}
	if ra.ShmSize > 0 {
		s.ShmSize = ra.ShmSize
	}
	if ra.User != "" {
		s.User = ra.User
	}
	s.CapDrop = append(s.CapDrop, ra.CapDrop...)
	s.Devices = append(s.Devices, ra.Devices...)
	s.ExtraHosts = append(s.ExtraHosts, ra.ExtraHosts...)
	for k, v := range ra.Sysctls {
		if s.Sysctls == nil {
			s.Sysctls = make(map[string]string)
		}
		s.Sysctls[k] = v
	}
	s.RunArgs = ra.Passthrough
}

// applyFeatureRequirements adds the capabilities, security options,
// privileged mode and init that features declare.
//
// Feature containerEnv is baked into the derived image as ENV, so it is not
// passed again here (that would override user containerEnv and freeze
// ${VAR} references unexpanded).
func (r *UnifiedRuntime) applyFeatureRequirements(s *ContainerSpec) {
	if len(r.resolved.Features) == 0 {
		return
	}
	reqs := features.GetSecurityRequirements(r.resolved.Features)
	if reqs.Privileged || len(reqs.Capabilities) > 0 {
		r.report.Warnf("Features require elevated permissions:")
		for _, name := range reqs.FeatureNames {
			r.report.Warnf("  - %s", name)
		}
	}
	s.CapAdd = append(s.CapAdd, reqs.Capabilities...)
	s.SecurityOpt = append(s.SecurityOpt, reqs.SecurityOpts...)
	if reqs.Privileged {
		s.Privileged = true
	}
	if features.NeedsInit(r.resolved.Features) {
		s.Init = true
	}
}

// overrideCommand reports whether the image/service command is replaced by
// a keep-alive. Per spec it defaults to true for image/Dockerfile plans and
// false for compose.
func (r *UnifiedRuntime) overrideCommand() bool {
	if r.resolved.RawConfig != nil && r.resolved.RawConfig.OverrideCommand != nil {
		return *r.resolved.RawConfig.OverrideCommand
	}
	_, isCompose := r.resolved.Plan.(*devcontainer.ComposePlan)
	return !isCompose
}

// buildLabels builds the container labels.
func (r *UnifiedRuntime) buildLabels() map[string]string {
	l := state.NewContainerLabels()
	l.WorkspaceID = r.resolved.ID
	l.WorkspaceName = r.resolved.Name
	l.WorkspacePath = r.resolved.LocalRoot
	l.ConfigPath = r.resolved.ConfigPath
	l.HashConfig = r.resolved.ConfigHash
	l.HashVersion = state.CurrentHashVersion
	l.LifecycleMarkers = true
	l.BuildMethod = string(r.resolved.Plan.Type())
	l.IsPrimary = true

	if r.resolved.BaseImage != "" {
		l.BaseImage = r.resolved.BaseImage
	}
	if r.derivedImage != "" {
		l.DerivedImage = r.derivedImage
	}

	// Set compose-specific labels
	if plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok {
		l.ComposeProject = plan.ProjectName
		l.ComposeService = plan.Service
	}

	// Store installed features
	if len(r.resolved.Features) > 0 {
		featureIDs := make([]string, len(r.resolved.Features))
		for i, f := range r.resolved.Features {
			featureIDs[i] = f.ID
		}
		l.FeaturesInstalled = featureIDs
	}

	return l.ToMap()
}

// buildMounts returns the configured mounts, with tmpfs mounts split out.
// /run/secrets is always a tmpfs: besides user runtime secrets it holds
// the dcx-managed SSH host private key, which must never land in the
// container's writable layer (visible to docker commit/export/diff).
func (r *UnifiedRuntime) buildMounts() ([]devcontainer.Mount, map[string]string) {
	var mounts []devcontainer.Mount
	tmpfs := map[string]string{agentproto.SecretsDir: "rw,noexec,nosuid,size=1m"}
	for _, m := range r.resolved.Mounts {
		if m.Type == "tmpfs" {
			tmpfs[m.Target] = ""
		} else {
			mounts = append(mounts, m)
		}
	}
	return mounts, tmpfs
}

// buildEnvironment builds the container environment: containerEnv plus
// DCX_PROJECT_NAME (for terminal titles and user scripts).
func (r *UnifiedRuntime) buildEnvironment() map[string]string {
	env := make(map[string]string, len(r.resolved.ContainerEnv)+1)
	for k, v := range r.resolved.ContainerEnv {
		env[k] = v
	}
	if r.resolved.Name != "" {
		env["DCX_PROJECT_NAME"] = r.resolved.Name
	}
	return env
}

// buildPortBindings combines forwardPorts, appPorts, and the dcx-agent SSH
// listener into a single slice. AppPorts are bound to localhost for
// security per the devcontainer spec. The SSH port is always published on
// 127.0.0.1 with a Docker-assigned ephemeral host port.
func (r *UnifiedRuntime) buildPortBindings() []devcontainer.PortForward {
	var ports []devcontainer.PortForward

	// Add forward ports (bind to all interfaces by default)
	ports = append(ports, r.resolved.ForwardPorts...)

	// Add app ports (bound to localhost for security)
	for _, ap := range r.resolved.AppPorts {
		if ap.Host == "" {
			ap.Host = "localhost"
		}
		ports = append(ports, ap)
	}

	// Always publish the dcx-agent SSH port. Single-transport: there is no
	// opt-out. The in-agent ConnCallback is what enforces "loopback-only by
	// default" — even when we bind 0.0.0.0 (e.g. via --hosts), the callback
	// stays the primary gate.
	ports = append(ports, r.sshPortBinding())

	return ports
}

// sshPortBinding returns the PortForward that publishes the dcx-agent's
// SSH listener.
//
// The host port is derived deterministically from the workspace ID so
// `dcx down` + `dcx up` returns the same (host, port) tuple — IDE
// known_hosts entries and user bookmarks stay valid across recreates.
// If the derived port is already bound by something else we fall back to
// a Docker-picked ephemeral and emit a warning.
//
// The bind address defaults to 127.0.0.1 but can be widened via
// UpOptions.SSHBindHost (driven by `dcx up --hosts`).
func (r *UnifiedRuntime) sshPortBinding() devcontainer.PortForward {
	host := r.sshBindHost
	if host == "" {
		host = "127.0.0.1"
	}
	pf := devcontainer.PortForward{
		ContainerPort: agentproto.ListenPort,
		Host:          host,
		Protocol:      "tcp",
		Label:         "dcx SSH (internal)",
		OnAutoForward: "ignore",
	}

	if id := r.resolved.ID; id != "" {
		candidate := dcxssh.DeterministicPort(id)
		if dcxssh.IsHostPortAvailable(candidate) {
			pf.HostPort = candidate
			pf.EphemeralHostPort = false
			return pf
		}
		r.report.Warnf("SSH: preferred port %d is busy; falling back to an ephemeral port for this workspace", candidate)
	}
	pf.EphemeralHostPort = true
	return pf
}

// Start implements ContainerRuntime.Start.
func (r *UnifiedRuntime) Start(ctx context.Context) error {
	if plan, ok := r.composePlan(); ok {
		return r.composeProject(plan).Start(ctx, r.engine)
	}
	return r.engine.StartContainer(ctx, r.containerName)
}

// Build implements ContainerRuntime.Build.
func (r *UnifiedRuntime) Build(ctx context.Context, opts BuildOptions) error {
	if r.resolved == nil {
		return fmt.Errorf("no resolved configuration - use NewUnifiedRuntime")
	}

	if plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok && len(r.resolved.Features) == 0 && !r.resolved.ShouldUpdateUID {
		args := []string{"build"}
		if opts.NoCache {
			args = append(args, "--no-cache")
		}
		if opts.Pull {
			args = append(args, "--pull")
		}
		return r.engine.Compose(ctx, r.userCompose(plan), args...)
	}

	image, err := r.buildImages(ctx, UpOptions{Build: true, Rebuild: opts.NoCache, Pull: opts.Pull})
	if err != nil {
		return err
	}
	if image != "" {
		r.report.Infof("Built image: %s", image)
	}
	return nil
}

// Compose helper methods

func (r *UnifiedRuntime) composePlan() (*devcontainer.ComposePlan, bool) {
	if r.resolved == nil {
		return nil, false
	}
	plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan)
	return plan, ok
}

// userCompose is the project with only the user's compose files: used to
// build and inspect the services before dcx's override applies.
func (r *UnifiedRuntime) userCompose(plan *devcontainer.ComposePlan) ComposeProject {
	name := plan.ProjectName
	if name == "" {
		name = r.containerName
	}
	return ComposeProject{
		Name:  name,
		Dir:   r.resolved.ConfigDir,
		Files: append([]string(nil), plan.Files...),
	}
}

// composeProject is the full project: the user's files plus dcx's
// persistent override, once it has been written.
func (r *UnifiedRuntime) composeProject(plan *devcontainer.ComposePlan) ComposeProject {
	p := r.userCompose(plan)
	if path, err := ComposeOverridePath(r.resolved.ID); err == nil {
		if _, err := os.Stat(path); err == nil {
			p = p.with(path)
		}
	}
	return p
}

// ComposeOverridePath is where dcx keeps the compose override of a
// workspace. It persists for the environment's lifetime (compose records
// it in the containers' config_files label), so later start/stop/down
// commands see the same project definition as `up`.
func ComposeOverridePath(workspaceID string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dcx", "compose", workspaceID, "docker-compose.dcx.yml"), nil
}

// writeComposeOverride renders spec for the primary service and writes it
// to the workspace's persistent override file.
func (r *UnifiedRuntime) writeComposeOverride(plan *devcontainer.ComposePlan, spec ContainerSpec) error {
	data, err := composeOverride(plan.Service, spec, nil)
	if err != nil {
		return err
	}
	path, err := ComposeOverridePath(r.resolved.ID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// writeTempOverride writes a one-off compose override (e.g. build secrets)
// and returns its path; the caller removes it.
func writeTempOverride(service string, spec ContainerSpec, buildSecrets map[string]string) (string, error) {
	data, err := composeOverride(service, spec, buildSecrets)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "dcx-override-*.yml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	_ = f.Close()
	return f.Name(), nil
}

func (r *UnifiedRuntime) ensureServicesBuilt(ctx context.Context, plan *devcontainer.ComposePlan, buildSecrets map[string]string) error {
	project := r.userCompose(plan)
	if len(buildSecrets) > 0 {
		path, err := writeTempOverride(plan.Service, ContainerSpec{}, buildSecrets)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(path) }()
		project = project.with(path)
	}
	return r.engine.Compose(ctx, project, "build")
}

func (r *UnifiedRuntime) getComposeBaseImage(ctx context.Context, plan *devcontainer.ComposePlan) (string, error) {
	if r.resolved.BaseImage != "" {
		return r.resolved.BaseImage, nil
	}

	if plan == nil {
		return "", fmt.Errorf("no compose configuration found")
	}

	if len(plan.Files) == 0 {
		return "", fmt.Errorf("no compose files specified")
	}

	// Use docker compose config to get fully resolved configuration
	output, err := r.engine.ComposeOutput(ctx, r.userCompose(plan), "config", "--format", "json")
	if err != nil {
		return "", fmt.Errorf("failed to get compose config: %w", err)
	}

	var config struct {
		Services map[string]struct {
			Image string `json:"image"`
		} `json:"services"`
		Name string `json:"name"`
	}

	if err := json.Unmarshal(output, &config); err != nil {
		return "", fmt.Errorf("failed to parse compose config: %w", err)
	}

	serviceName := plan.Service
	if serviceName == "" {
		return "", fmt.Errorf("no primary service specified")
	}

	svc, ok := config.Services[serviceName]
	if !ok {
		return "", fmt.Errorf("service %q not found in compose file", serviceName)
	}

	if svc.Image != "" {
		return svc.Image, nil
	}

	// Service uses build - compute built image name
	projectName := plan.ProjectName
	if projectName == "" {
		projectName = config.Name
	}
	if projectName == "" {
		projectName = r.resolved.ServiceName
	}
	return fmt.Sprintf("%s-%s", projectName, serviceName), nil
}
