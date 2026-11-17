package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/build"
	"github.com/griffithind/dcx/internal/dcxhome"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/report"
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

	containerName string

	derivedImage string
	// ssh is the agent endpoint of the container being created (planSSH).
	ssh *sshEndpoint
	// composeInfo caches composeService.
	composeInfo *composeServiceInfo
	// ports caches buildPortBindings (it reports what it skips).
	ports []devcontainer.PortForward
	// command is what a chained feature entrypoint execs (prepareCommand).
	command []string
	// selinux is set when the daemon labels containers for SELinux.
	selinux bool
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

// Up builds the images and creates and starts the environment.
func (r *Provisioner) Up(ctx context.Context, opts UpOptions) error {
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
	// With SELinux labelling in the daemon, the workspace bind mount must
	// be relabelled to be readable in the container.
	if info, err := r.engine.Info(ctx); err == nil {
		r.selinux = info.SELinux
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

	// The built image's label: the FROM image's metadata plus the local
	// config (a label set here replaces the one the build inherits).
	metadata, _ := build.GenerateMetadataLabel(r.resolved.MetadataLabel, nil, r.resolved.Local)

	// With both image and build, the built image is also tagged as image
	// (the reference implementation's behaviour).
	var extraTags []string
	if r.resolved.Local != nil && r.resolved.Local.Image != "" {
		extraTags = []string{r.resolved.Local.Image}
	}

	_, err := r.builder.BuildFromDockerfile(ctx, build.DockerfileBuildOptions{
		Tag:        imageTag,
		ExtraTags:  extraTags,
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
		// Without a containerUser, features install for the compose
		// service's user (or the image's USER, which baseID covers).
		serviceUser := ""
		if plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok {
			info, err := r.composeService(ctx, plan)
			if err != nil {
				return "", err
			}
			serviceUser = info.User
		}
		tag := r.layerTag("features", featuresLayerVersion, baseID, devcontainer.FeaturesKey(r.resolved.Features),
			r.resolved.RemoteUser, r.resolved.ContainerUser, serviceUser)

		// The base metadata of the features image is the one resolution
		// read: for a Dockerfile plan the built image's own label already
		// holds the local config, which would then appear twice.
		baseImageMetadata := r.resolved.MetadataLabel
		if r.resolved.MetadataImage == "" {
			if labels, err := r.engine.ImageLabels(ctx, image); err == nil {
				baseImageMetadata = labels[devcontainer.DevcontainerMetadataLabel]
			}
		}
		image, err = r.builder.BuildWithFeatures(ctx, build.FeatureBuildOptions{
			BaseImage:         image,
			Tag:               tag,
			Features:          r.resolved.Features,
			RemoteUser:        r.resolved.RemoteUser,
			ContainerUser:     r.resolved.ContainerUser,
			ServiceUser:       serviceUser,
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
		// The layer ends with the image's own USER, as the reference
		// implementation does; it used to end with containerUser or the
		// remote user, which made the keep-alive, feature entrypoints and
		// compose commands run as the remote user. containerUser applies at
		// runtime anyway (ContainerSpec.User).
		imageCfg, err := r.engine.ImageConfig(ctx, image)
		if err != nil {
			return "", fmt.Errorf("failed to inspect image %s: %w", image, err)
		}
		imageUser := imageCfg.User
		if imageUser == "" {
			imageUser = "root"
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

// featuresLayerVersion changes when the generated features Dockerfile
// changes how features install, so existing features images are not reused.
const featuresLayerVersion = "2"

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

	r.warnElevatedFeatures()

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

// workspaceMount returns the resolved workspace mount (nil for none),
// relabelled for SELinux when Docker labels containers.
func (r *Provisioner) workspaceMount() *devcontainer.Mount {
	if r.resolved.WorkspaceMount == nil {
		return nil
	}
	m := *r.resolved.WorkspaceMount
	if r.selinux && (m.Type == "" || m.Type == "bind") {
		m.Relabel = true
	}
	return &m
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
	if ra.Name != "" {
		s.Name = ra.Name
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

// warnElevatedFeatures says which features need elevated permissions. The
// permissions themselves (capAdd, securityOpt, privileged, init) were
// merged into the resolved configuration by the builder; adding them again
// here emitted every one twice.
func (r *Provisioner) warnElevatedFeatures() {
	if names := features.ElevatedFeatures(r.resolved.Features); len(names) > 0 {
		r.report.Warnf("Features require elevated permissions:")
		for _, name := range names {
			r.report.Warnf("  - %s", name)
		}
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
	l.HashInputs = r.resolved.InputsHash
	if r.resolved.Config != nil {
		l.ShutdownAction = r.resolved.Config.ShutdownAction
	}
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

	labels := l.ToMap()
	// The container's devcontainer.metadata is the merged metadata (image,
	// features, devcontainer.json), as the reference implementation records
	// it, so tools that attach to a running container (they read the
	// container's label) see remoteUser and the rest even when no dcx
	// layer was built.
	if metadata, err := build.GenerateMetadataLabel(r.resolved.MetadataLabel, r.resolved.Features, r.resolved.Local); err == nil && metadata != "" {
		labels[devcontainer.DevcontainerMetadataLabel] = metadata
	}
	return labels
}

// tmpfsOptions translates a tmpfs mount's --mount options (tmpfs-size,
// tmpfs-mode) into --tmpfs / compose options ("size=64m,mode=1777").
func tmpfsOptions(m devcontainer.Mount) string {
	var opts []string
	if v := m.Option("tmpfs-size"); v != "" {
		opts = append(opts, "size="+v)
	}
	if v := m.Option("tmpfs-mode"); v != "" {
		opts = append(opts, "mode="+v)
	}
	return strings.Join(opts, ",")
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
			tmpfs[m.Target] = tmpfsOptions(m)
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
// user's appPort and forwardPorts, then the dcx-agent's port.
//
// forwardPorts without an explicit host address bind 127.0.0.1 (a dev
// server or database should not be reachable from the network unless asked
// for, "0.0.0.0:3000:3000"); appPort numbers bind 127.0.0.1 and appPort
// strings are published as written (see Builder.parsePorts). An entry is
// skipped when an earlier one already publishes its host port (3000 in both
// appPort and forwardPorts, or "localhost:3000" next to 3000), or when the
// compose file publishes that host port on the service that owns the
// network ("3000:3000" in the compose file and forwardPorts 3000).
//
// With host networking nothing is published (the ports are on the host);
// with a compose network owner they are published there (see
// writeComposeOverride).
func (r *Provisioner) buildPortBindings() []devcontainer.PortForward {
	if r.ssh != nil && !r.ssh.publish {
		return nil
	}
	if r.ports != nil {
		return r.ports
	}
	composePorts := r.composePublishedPorts()
	ports := []devcontainer.PortForward{}
	add := func(field string, p devcontainer.PortForward) {
		for _, q := range composePorts {
			if q.Collides(p) {
				if q.SamePort(p) {
					r.report.Infof("%s: %s is already published by the compose file", field, p.DockerSpec())
				} else {
					r.report.Warnf("%s: skipping %s: the compose file publishes the same host port (%s)", field, p.DockerSpec(), q.DockerSpec())
				}
				return
			}
		}
		for _, q := range ports {
			if q.Collides(p) {
				if !q.SamePort(p) {
					r.report.Warnf("%s: skipping %s: its host port is already published (%s)", field, p.DockerSpec(), q.DockerSpec())
				}
				return
			}
		}
		ports = append(ports, p)
	}
	for _, p := range r.resolved.AppPorts {
		add("appPort", p)
	}
	for _, p := range r.resolved.ForwardPorts {
		if p.Host == "" {
			p.Host = "127.0.0.1"
		}
		add("forwardPorts", p)
	}
	if r.ssh != nil {
		ports = append(ports, r.ssh.portForward())
	}
	r.ports = ports
	return ports
}

// composePublishedPorts returns the ports the user's compose files publish
// on the service that owns the primary container's network: the network
// owner for network_mode service:<owner>, else the primary service. It is
// nil for single containers and before composeService has run (planSSH
// runs it).
func (r *Provisioner) composePublishedPorts() []devcontainer.PortForward {
	plan, ok := r.composePlan()
	if !ok || r.composeInfo == nil {
		return nil
	}
	service := plan.Service
	if r.ssh != nil && r.ssh.owner != "" {
		service = r.ssh.owner
	}
	return r.composeInfo.Ports[service]
}

// containerPorts is what the primary container publishes itself: nothing
// when it shares a compose network owner's namespace.
func (r *Provisioner) containerPorts() []devcontainer.PortForward {
	if r.ssh != nil && r.ssh.owner != "" {
		return nil
	}
	return r.buildPortBindings()
}

// Start starts the existing, stopped environment.
func (r *Provisioner) Start(ctx context.Context) error {
	if plan, ok := r.composePlan(); ok {
		return r.composeProject(plan).Start(ctx, r.engine)
	}
	return r.engine.StartContainer(ctx, r.containerName)
}

// Build builds the images without creating anything.
func (r *Provisioner) Build(ctx context.Context, opts BuildOptions) error {
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
	return dcxhome.Path("compose", workspaceID, "docker-compose.dcx.yml")
}

// writeComposeOverride renders spec for the primary service and writes it
// to the workspace's persistent override file.
func (r *Provisioner) writeComposeOverride(plan *devcontainer.ComposePlan, spec ContainerSpec) error {
	file := overrideFile(plan.Service, spec, nil)
	// A volume the user's compose files declare is theirs (usually
	// project-prefixed); re-declaring it with a name would switch it to
	// another volume.
	if r.composeInfo != nil {
		for name := range r.composeInfo.Volumes {
			delete(file.Volumes, esc(name))
		}
	}
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
	User        string // the service's user: (features install for it)
	ProjectName string // the compose files' top-level name
	// Entrypoint and Command are the service's own (nil: not set).
	Entrypoint []string
	Command    []string
	// Ports are the ports each service publishes.
	Ports map[string][]devcontainer.PortForward
	// Volumes are the top-level volumes the compose files declare.
	Volumes map[string]bool
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
			Ports       []composePort   `json:"ports"`
			User        string          `json:"user"`
		} `json:"services"`
		Name    string                     `json:"name"`
		Volumes map[string]json.RawMessage `json:"volumes"`
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
		User:        svc.User,
		ProjectName: config.Name,
		Entrypoint:  composeArgv(svc.Entrypoint),
		Command:     composeArgv(svc.Command),
		Ports:       map[string][]devcontainer.PortForward{},
		Volumes:     map[string]bool{},
	}
	for name, s := range config.Services {
		for _, p := range s.Ports {
			if pf, ok := p.forward(); ok {
				r.composeInfo.Ports[name] = append(r.composeInfo.Ports[name], pf)
			}
		}
	}
	for name := range config.Volumes {
		r.composeInfo.Volumes[name] = true
	}
	return r.composeInfo, nil
}

// composePort is a published port in `docker compose config` output.
type composePort struct {
	Target    int    `json:"target"`
	Published string `json:"published"`
	HostIP    string `json:"host_ip"`
	Protocol  string `json:"protocol"`
}

// forward converts a published port (false unless it is published on a
// fixed host port: not for a Docker-assigned port or a range).
func (p composePort) forward() (devcontainer.PortForward, bool) {
	n, err := strconv.Atoi(p.Published)
	if err != nil || p.Target <= 0 {
		return devcontainer.PortForward{}, false
	}
	return devcontainer.PortForward{ContainerPort: p.Target, HostPort: n, Host: p.HostIP, Protocol: p.Protocol}, true
}
