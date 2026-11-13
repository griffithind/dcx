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
	"github.com/griffithind/dcx/internal/state"
)

// Provisioner builds the environment's images (base, features, UID layers)
// and creates or starts its containers from a ContainerSpec, for image,
// Dockerfile and compose plans alike. Stopping and removing existing
// environments is the service's job (it works from labels, not config).
type Provisioner struct {
	engine   Engine
	report   report.Reporter
	resolved *devcontainer.ResolvedDevContainer
	builder  *build.Builder

	containerID   string
	containerName string

	derivedImage string
	// ssh is the agent endpoint of the container being created (planSSH).
	ssh *sshEndpoint
	// composeInfo caches composeService.
	composeInfo *composeServiceInfo
	// command is what a chained feature entrypoint execs (prepareCommand).
	command []string
}

// NewProvisioner creates a new runtime for a resolved devcontainer.
func NewProvisioner(engine Engine, rep report.Reporter, resolved *devcontainer.ResolvedDevContainer) (*Provisioner, error) {
	if resolved == nil {
		return nil, fmt.Errorf("resolved devcontainer is required")
	}

	rep = report.OrDiscard(rep)
	builder := build.NewBuilder(engine, rep)

	return &Provisioner{
		engine:        engine,
		report:        rep,
		resolved:      resolved,
		builder:       builder,
		containerName: resolved.ResourceName,
	}, nil
}

// Up implements ContainerRuntime.Up.
func (r *Provisioner) Up(ctx context.Context, opts UpOptions) error {
	if r.resolved == nil {
		return fmt.Errorf("no resolved configuration - use NewProvisioner")
	}

	// Determine the approach based on plan type
	switch plan := r.resolved.Plan.(type) {
	case *devcontainer.ComposePlan:
		return r.upCompose(ctx, opts, plan)
	case *devcontainer.ImagePlan, *devcontainer.DockerfilePlan:
		return r.upSingle(ctx, opts)
	default:
		return fmt.Errorf("unsupported plan type: %T", plan)
	}
}

// upCompose handles compose-based configurations.
func (r *Provisioner) upCompose(ctx context.Context, opts UpOptions, plan *devcontainer.ComposePlan) error {
	image, err := r.buildImages(ctx, opts)
	if err != nil {
		return err
	}
	if err := r.planSSH(ctx, opts.SSHAccess); err != nil {
		return err
	}
	if err := r.prepareCommand(ctx, image); err != nil {
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
	// Let compose rebuild only when it runs the service image itself. When
	// dcx layered the image (features or the UID update), the override
	// points the service at dcx's tag, and `--build` would rebuild the
	// user's Dockerfile straight onto that tag, dropping dcx's layers.
	// buildImages has already rebuilt the services in that case.
	if opts.Build && r.derivedImage == "" {
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
func (r *Provisioner) upSingle(ctx context.Context, opts UpOptions) error {
	finalImage, err := r.buildImages(ctx, opts)
	if err != nil {
		return err
	}
	if err := r.planSSH(ctx, opts.SSHAccess); err != nil {
		return err
	}
	if err := r.prepareCommand(ctx, finalImage); err != nil {
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

// BuildImages builds (or pulls) everything Up needs without creating
// anything, so a recreate can prepare the new images while the old
// environment still runs: a failing build then leaves it in place.
func (r *Provisioner) BuildImages(ctx context.Context, opts UpOptions) error {
	// An unreachable network mode fails here too, before a recreate
	// removes the old environment.
	if _, _, err := r.sshNetwork(ctx); err != nil {
		return err
	}
	image, err := r.buildImages(ctx, opts)
	if err != nil {
		return err
	}
	// A compose plan without dcx layers builds nothing in buildImages
	// (compose runs the service image itself); build its services here so
	// the later `compose up` uses fresh images.
	if plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok && image == "" {
		if err := r.ensureServicesBuilt(ctx, plan, opts.BuildSecrets); err != nil {
			return fmt.Errorf("failed to build services: %w", err)
		}
	}
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
func (r *Provisioner) buildImages(ctx context.Context, opts UpOptions) (string, error) {
	if plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok {
		if len(r.resolved.Features) == 0 && !r.resolved.ShouldUpdateUID {
			return "", nil
		}
		if err := r.ensureServicesBuilt(ctx, plan, opts.BuildSecrets); err != nil {
			return "", fmt.Errorf("failed to build services: %w", err)
		}
		baseImage, built, err := r.getComposeBaseImage(ctx, plan)
		if err != nil {
			return "", fmt.Errorf("failed to determine base image: %w", err)
		}
		// `compose build` skips image-only services, so their image may not
		// be local yet; dcx layers on top of it and needs it present.
		if !built {
			if err := r.ensureImage(ctx, baseImage, opts.Pull); err != nil {
				return "", err
			}
		}
		finalImage, err := r.layerImages(ctx, baseImage, opts)
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
	finalImage, err := r.layerImages(ctx, baseImage, opts)
	if err != nil {
		return "", err
	}
	if finalImage != baseImage {
		r.derivedImage = finalImage
	}
	return finalImage, nil
}

// ensureImage makes sure ref is present locally, pulling it when missing
// (or always, with pull).
func (r *Provisioner) ensureImage(ctx context.Context, ref string, pull bool) error {
	exists, err := r.engine.ImageExists(ctx, ref)
	if err != nil {
		return fmt.Errorf("failed to check image: %w", err)
	}
	if !exists || pull {
		r.report.Infof("Pulling image: %s", ref)
		if err := r.engine.PullImageWithProgress(ctx, ref, os.Stdout); err != nil {
			return fmt.Errorf("failed to pull image: %w", err)
		}
	}
	return nil
}

// resolveBaseImage determines the base image for single-container configs.
func (r *Provisioner) resolveBaseImage(ctx context.Context, opts UpOptions) (string, error) {
	switch plan := r.resolved.Plan.(type) {
	case *devcontainer.ImagePlan:
		r.report.Infof("Using image: %s", plan.Image)
		if err := r.ensureImage(ctx, plan.Image, opts.Pull); err != nil {
			return "", err
		}
		return plan.Image, nil

	case *devcontainer.DockerfilePlan:
		imageTag := fmt.Sprintf("%s%s:base", ImageTagPrefix, r.resolved.WorkspaceID)
		r.report.Infof("Building image: %s", imageTag)

		if err := r.buildDockerfile(ctx, imageTag, plan, opts); err != nil {
			return "", fmt.Errorf("failed to build image: %w", err)
		}

		return imageTag, nil
	}

	return "", fmt.Errorf("no image or build configuration found")
}

// buildDockerfile builds an image from a Dockerfile using the CLI.
// buildDockerfile builds the user's Dockerfile. --pull refreshes its FROM
// image and --no-cache (dcx build) bypasses BuildKit's cache.
func (r *Provisioner) buildDockerfile(ctx context.Context, imageTag string, plan *devcontainer.DockerfilePlan, opts UpOptions) error {
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
	metadata, _ := build.GenerateMetadataLabel("", nil, r.resolved.Local)

	_, err := r.builder.BuildFromDockerfile(ctx, build.DockerfileBuildOptions{
		Tag:        imageTag,
		Dockerfile: dockerfilePath,
		Context:    buildCtx,
		Args:       buildArgs,
		Target:     plan.Target,
		CacheFrom:  plan.CacheFrom,
		Progress:   os.Stdout,
		Metadata:   metadata,
		Secrets:    opts.BuildSecrets,
		Options:    plan.Options,
		Pull:       opts.Pull,
		NoCache:    opts.NoCache,
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
func (r *Provisioner) layerImages(ctx context.Context, baseImage string, opts UpOptions) (string, error) {
	rebuild := opts.Rebuild
	image := baseImage

	if len(r.resolved.Features) > 0 {
		baseID, err := r.engine.ImageID(ctx, image)
		if err != nil {
			return "", fmt.Errorf("failed to inspect base image %s: %w", image, err)
		}
		tag := r.layerTag("features", baseID, devcontainer.FeaturesKey(r.resolved.Features),
			r.resolved.RemoteUser, r.resolved.ContainerUser)

		baseImageMetadata := ""
		if labels, err := r.engine.ImageLabels(ctx, image); err == nil {
			baseImageMetadata = labels[devcontainer.DevcontainerMetadataLabel]
		}
		image, err = r.builder.BuildWithFeatures(ctx, build.FeatureBuildOptions{
			BaseImage:         image,
			Tag:               tag,
			Features:          r.resolved.Features,
			RemoteUser:        r.resolved.RemoteUser,
			ContainerUser:     r.resolved.ContainerUser,
			Rebuild:           rebuild,
			NoCache:           opts.NoCache,
			Progress:          os.Stdout,
			BaseImageMetadata: baseImageMetadata,
			LocalConfig:       r.resolved.Local,
		})
		if err != nil {
			return "", fmt.Errorf("failed to build derived image with features: %w", err)
		}
	}

	if r.resolved.ShouldUpdateUID {
		imageID, err := r.engine.ImageID(ctx, image)
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
func (r *Provisioner) layerTag(kind string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("%s%s:%s-%s", ImageTagPrefix, r.resolved.WorkspaceID, kind,
		hex.EncodeToString(sum[:])[:hashTruncationLength])
}

// createContainer creates the primary container from imageRef.
func (r *Provisioner) createContainer(ctx context.Context, imageRef string) (string, error) {
	return r.engine.CreateContainer(ctx, r.spec(imageRef))
}

// spec builds the ContainerSpec of the primary container. It is the single
// place runtime settings are derived from the resolved configuration, for
// both single-container and compose plans.
//
// Settings that only exist for `docker run` are left out for compose: the
// container name and working directory (compose owns them), the workspace
// mount (the compose file defines it, per the spec) and runArgs.
func (r *Provisioner) spec(image string) ContainerSpec {
	res := r.resolved
	_, isCompose := res.Plan.(*devcontainer.ComposePlan)

	s := ContainerSpec{
		Image:       image,
		User:        res.ContainerUser,
		Labels:      r.buildLabels(),
		Env:         r.buildEnvironment(),
		Ports:       r.containerPorts(),
		CapAdd:      append([]string(nil), res.CapAdd...),
		SecurityOpt: append([]string(nil), res.SecurityOpt...),
		Privileged:  res.Privileged,
		Init:        res.Init,
	}
	s.Mounts, s.Tmpfs = r.buildMounts()

	if !isCompose {
		s.Name = res.ResourceName
		s.WorkingDir = res.WorkspaceFolder
		if wm := r.workspaceMount(); wm != nil {
			s.Mounts = append([]devcontainer.Mount{*wm}, s.Mounts...)
		}
		r.applyRunArgs(&s)
	}

	r.applyFeatureRequirements(&s)

	if gpu := res.GPU; gpu.Wanted() {
		if gpu.Count > 0 {
			s.GPUs = strconv.Itoa(gpu.Count)
		} else {
			s.GPUs = "all"
		}
	}

	switch entrypoints := r.featureEntrypoints(); {
	case r.resolved.OverrideCommand:
		s.Entrypoint = keepAlive(entrypoints)
	case len(entrypoints) > 0:
		s.Entrypoint = chainEntrypoints(entrypoints)
		s.Cmd = r.command
	}
	return s
}

// workspaceMount returns the workspace mount of a single-container plan:
// workspaceMount from the config, or a bind of the local workspace.
func (r *Provisioner) workspaceMount() *devcontainer.Mount {
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
func (r *Provisioner) applyRunArgs(s *ContainerSpec) {
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
func (r *Provisioner) applyFeatureRequirements(s *ContainerSpec) {
	if len(r.resolved.Features) == 0 {
		return
	}
	reqs := features.CollectSecurityRequirements(r.resolved.Features)
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

// buildLabels builds the container labels.
func (r *Provisioner) buildLabels() map[string]string {
	l := state.NewContainerLabels()
	l.WorkspaceID = r.resolved.WorkspaceID
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
	if r.ssh != nil {
		r.ssh.labels(l)
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
func (r *Provisioner) buildMounts() ([]devcontainer.Mount, map[string]string) {
	var mounts []devcontainer.Mount
	tmpfs := map[string]string{agentproto.SecretsDir: "rw,noexec,nosuid,size=" + strconv.Itoa(SecretsTmpfsMiB) + "m"}
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
func (r *Provisioner) buildEnvironment() map[string]string {
	env := make(map[string]string, len(r.resolved.ContainerEnv)+1)
	for k, v := range r.resolved.ContainerEnv {
		env[k] = v
	}
	// The sanitized name, as dcx has always set it (the display name may
	// contain spaces).
	if slug := devcontainer.SanitizeProjectName(r.resolved.Name); slug != "" {
		env["DCX_PROJECT_NAME"] = slug
	}
	return env
}

// buildPortBindings returns the ports published for the environment: the
// user's forwardPorts and appPort, then the dcx-agent's port. Ports without
// an explicit host address bind 127.0.0.1: forwardPorts are forwarded to
// the local machine in the spec, and a dev server or database should not
// be reachable from the network unless asked for ("0.0.0.0:3000:3000").
// With host networking nothing is published (the ports are on the host);
// with a compose network owner they are published there (see
// writeComposeOverride).
func (r *Provisioner) buildPortBindings() []devcontainer.PortForward {
	if r.ssh != nil && !r.ssh.publish {
		return nil
	}
	var ports []devcontainer.PortForward
	for _, group := range [][]devcontainer.PortForward{r.resolved.ForwardPorts, r.resolved.AppPorts} {
		for _, p := range group {
			if p.Host == "" {
				p.Host = "127.0.0.1"
			}
			ports = append(ports, p)
		}
	}
	if r.ssh != nil {
		ports = append(ports, r.ssh.portForward())
	}
	return ports
}

// containerPorts is what the primary container publishes itself: nothing
// when it shares a compose network owner's namespace.
func (r *Provisioner) containerPorts() []devcontainer.PortForward {
	if r.ssh != nil && r.ssh.owner != "" {
		return nil
	}
	return r.buildPortBindings()
}

// Start implements ContainerRuntime.Start.
func (r *Provisioner) Start(ctx context.Context) error {
	if plan, ok := r.composePlan(); ok {
		return r.composeProject(plan).Start(ctx, r.engine)
	}
	return r.engine.StartContainer(ctx, r.containerName)
}

// Build implements ContainerRuntime.Build.
func (r *Provisioner) Build(ctx context.Context, opts BuildOptions) error {
	if r.resolved == nil {
		return fmt.Errorf("no resolved configuration - use NewProvisioner")
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

	image, err := r.buildImages(ctx, UpOptions{Build: true, Rebuild: opts.NoCache, NoCache: opts.NoCache, Pull: opts.Pull})
	if err != nil {
		return err
	}
	if image != "" {
		r.report.Infof("Built image: %s", image)
	}
	return nil
}

// Compose helper methods

func (r *Provisioner) composePlan() (*devcontainer.ComposePlan, bool) {
	if r.resolved == nil {
		return nil, false
	}
	plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan)
	return plan, ok
}

// userCompose is the project with only the user's compose files: used to
// build and inspect the services before dcx's override applies.
func (r *Provisioner) userCompose(plan *devcontainer.ComposePlan) ComposeProject {
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
func (r *Provisioner) composeProject(plan *devcontainer.ComposePlan) ComposeProject {
	p := r.userCompose(plan)
	if path, err := ComposeOverridePath(r.resolved.WorkspaceID); err == nil {
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
func (r *Provisioner) writeComposeOverride(plan *devcontainer.ComposePlan, spec ContainerSpec) error {
	file := overrideFile(plan.Service, spec, nil)
	// network_mode service:<owner>: every port (the user's and the
	// agent's) is published on the owner, the only container Docker
	// publishes ports on for that namespace.
	if r.ssh != nil && r.ssh.owner != "" {
		var owned []string
		for _, p := range r.buildPortBindings() {
			owned = append(owned, p.DockerSpec())
		}
		file.Services[r.ssh.owner] = composeService{Ports: owned}
	}
	data, err := marshalOverride(file)
	if err != nil {
		return err
	}
	path, err := ComposeOverridePath(r.resolved.WorkspaceID)
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

func (r *Provisioner) ensureServicesBuilt(ctx context.Context, plan *devcontainer.ComposePlan, buildSecrets map[string]string) error {
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

// getComposeBaseImage returns the primary service's image and whether
// compose builds it (a `build:` section) rather than pulling it.
func (r *Provisioner) getComposeBaseImage(ctx context.Context, plan *devcontainer.ComposePlan) (string, bool, error) {
	if r.resolved.BaseImage != "" {
		return r.resolved.BaseImage, false, nil
	}
	svc, err := r.composeService(ctx, plan)
	if err != nil {
		return "", false, err
	}
	if svc.Image != "" {
		return svc.Image, svc.Built, nil
	}

	// Service uses build - compute built image name
	projectName := plan.ProjectName
	if projectName == "" {
		projectName = svc.ProjectName
	}
	if projectName == "" {
		projectName = r.resolved.ResourceName
	}
	return fmt.Sprintf("%s-%s", projectName, plan.Service), true, nil
}

// composeArgv reads a compose entrypoint/command: a list, or a string in
// shell form; nil when unset.
func composeArgv(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var argv []string
	if json.Unmarshal(raw, &argv) == nil {
		return argv
	}
	var shell string
	if json.Unmarshal(raw, &shell) == nil {
		return []string{"/bin/sh", "-c", shell}
	}
	return nil
}

// composeServiceInfo is what dcx reads from the user's compose files about
// the primary service.
type composeServiceInfo struct {
	Image       string
	Built       bool // the service has a build: section
	NetworkMode string
	ProjectName string // the compose files' top-level name
	// Entrypoint and Command are the service's own (nil: not set).
	Entrypoint []string
	Command    []string
}

// composeService reads the primary service from the user's compose files
// (`docker compose config`, so includes, extends and variables are
// resolved). The result is cached for the provisioner's lifetime.
func (r *Provisioner) composeService(ctx context.Context, plan *devcontainer.ComposePlan) (*composeServiceInfo, error) {
	if r.composeInfo != nil {
		return r.composeInfo, nil
	}
	if len(plan.Files) == 0 {
		return nil, fmt.Errorf("no compose files specified")
	}
	if plan.Service == "" {
		return nil, fmt.Errorf("no primary service specified")
	}
	output, err := r.engine.ComposeOutput(ctx, r.userCompose(plan), "config", "--format", "json")
	if err != nil {
		return nil, fmt.Errorf("failed to get compose config: %w", err)
	}
	var config struct {
		Services map[string]struct {
			Image       string          `json:"image"`
			Build       json.RawMessage `json:"build"`
			NetworkMode string          `json:"network_mode"`
			Entrypoint  json.RawMessage `json:"entrypoint"`
			Command     json.RawMessage `json:"command"`
		} `json:"services"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(output, &config); err != nil {
		return nil, fmt.Errorf("failed to parse compose config: %w", err)
	}
	svc, ok := config.Services[plan.Service]
	if !ok {
		return nil, fmt.Errorf("service %q not found in compose file", plan.Service)
	}
	r.composeInfo = &composeServiceInfo{
		Image:       svc.Image,
		Built:       len(svc.Build) > 0 && string(svc.Build) != "null",
		NetworkMode: svc.NetworkMode,
		ProjectName: config.Name,
		Entrypoint:  composeArgv(svc.Entrypoint),
		Command:     composeArgv(svc.Command),
	}
	return r.composeInfo, nil
}
