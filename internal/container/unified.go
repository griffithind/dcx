package container

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/build"
	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
)

// UnifiedRuntime implements ContainerRuntime for all plan types.
// It handles image-based, Dockerfile-based, and compose-based devcontainers
// through a single unified implementation.
type UnifiedRuntime struct {
	resolved *devcontainer.ResolvedDevContainer
	builder  build.ImageBuilder // CLI-based image builder

	// Cached state
	containerID   string
	containerName string

	// Runtime state
	overridePath string
	derivedImage string

	// For lightweight existing container operations
	workspacePath  string
	composeProject string   // Set when working with existing compose environment
	isCompose      bool     // Whether this is a compose environment
	compose        *Compose // Compose client for compose operations
}

// NewUnifiedRuntime creates a new runtime for a resolved devcontainer.
func NewUnifiedRuntime(resolved *devcontainer.ResolvedDevContainer) (*UnifiedRuntime, error) {
	if resolved == nil {
		return nil, fmt.Errorf("resolved devcontainer is required")
	}

	// Create CLI-based image builder
	builder := build.NewCLIBuilder()

	return &UnifiedRuntime{
		resolved:      resolved,
		builder:       builder,
		containerName: resolved.ServiceName,
	}, nil
}

// NewUnifiedRuntimeForExistingCompose creates a lightweight runtime for existing compose environments.
// The configDir parameter should be the directory containing devcontainer.json (and typically the compose files).
func NewUnifiedRuntimeForExistingCompose(configDir, composeProject string) *UnifiedRuntime {
	return &UnifiedRuntime{
		workspacePath:  configDir, // Use configDir as working dir for compose commands
		composeProject: composeProject,
		isCompose:      true,
		compose:        ComposeClient(configDir, composeProject),
	}
}

// Up implements ContainerRuntime.Up.
func (r *UnifiedRuntime) Up(ctx context.Context, opts UpOptions) error {
	if r.resolved == nil {
		return fmt.Errorf("no resolved configuration - use NewUnifiedRuntime")
	}

	hasFeatures := len(r.resolved.Features) > 0

	// Determine the approach based on plan type
	switch plan := r.resolved.Plan.(type) {
	case *devcontainer.ComposePlan:
		return r.upCompose(ctx, opts, hasFeatures, plan)
	case *devcontainer.ImagePlan, *devcontainer.DockerfilePlan:
		return r.upSingle(ctx, opts, hasFeatures)
	default:
		return fmt.Errorf("unsupported plan type: %T", plan)
	}
}

// upCompose handles compose-based configurations.
func (r *UnifiedRuntime) upCompose(ctx context.Context, opts UpOptions, hasFeatures bool, plan *devcontainer.ComposePlan) error {
	// Build derived image with features if needed
	if hasFeatures {
		// Check if derived image is already cached before building compose services
		derivedTag := r.getDerivedImageTag()
		needsBuild := opts.Rebuild || !r.derivedImageExists(ctx, derivedTag)

		if needsBuild {
			// Only build compose services if we need to build a new derived image
			if err := r.ensureServicesBuilt(ctx, plan, opts.BuildSecrets); err != nil {
				return fmt.Errorf("failed to build services: %w", err)
			}
		}

		if err := r.buildDerivedImageForCompose(ctx, opts, plan); err != nil {
			return fmt.Errorf("failed to build derived image with features: %w", err)
		}
	} else {
		// Even without features, we may need to apply UID update layer for compose
		if err := r.applyUIDUpdateForCompose(ctx, opts, plan); err != nil {
			return fmt.Errorf("failed to apply UID update: %w", err)
		}
	}

	// Generate override file
	override, err := r.generateComposeOverride(plan, opts.BuildSecrets)
	if err != nil {
		return fmt.Errorf("failed to generate override: %w", err)
	}

	// Write override to temp file
	r.overridePath, err = r.writeToTempFile(override, "dcx-override-*.yml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(r.overridePath) }()

	// Build compose args
	args := r.composeBaseArgs(plan)
	args = append(args, "up", "-d")

	// Add --build only if explicitly requested and we DON'T have features
	if opts.Build && !hasFeatures {
		args = append(args, "--build")
	}

	// Determine which services to start
	if len(plan.RunServices) > 0 {
		args = append(args, plan.RunServices...)
	}

	return r.runCompose(ctx, args)
}

// upSingle handles single-container configurations (image or Dockerfile).
func (r *UnifiedRuntime) upSingle(ctx context.Context, opts UpOptions, hasFeatures bool) error {
	// Build derived image with features if needed
	var finalImage string
	if hasFeatures {
		// Check if derived image is already cached before building base image
		derivedTag := r.getDerivedImageTag()
		if !opts.Rebuild && r.derivedImageExists(ctx, derivedTag) {
			fmt.Printf("Using cached derived image\n")
			finalImage = derivedTag
			r.derivedImage = derivedTag
		} else {
			// Need to build - resolve base image first
			baseImage, err := r.resolveBaseImage(ctx, opts)
			if err != nil {
				return err
			}
			derivedImage, err := r.buildDerivedImage(ctx, baseImage, opts.Rebuild)
			if err != nil {
				return fmt.Errorf("failed to build derived image with features: %w", err)
			}
			finalImage = derivedImage
			r.derivedImage = derivedImage
		}
	} else {
		// No features - resolve base image
		baseImage, err := r.resolveBaseImage(ctx, opts)
		if err != nil {
			return err
		}
		finalImage = baseImage
		// Even without features, we may need to apply UID update layer
		uidImage, err := r.applyUIDUpdateLayer(ctx, baseImage, opts.Rebuild)
		if err != nil {
			return fmt.Errorf("failed to apply UID update: %w", err)
		}
		if uidImage != baseImage {
			finalImage = uidImage
			r.derivedImage = uidImage
		}
	}

	// Create the container
	containerID, err := r.createContainer(ctx, finalImage)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Start the container
	if err := MustDocker().StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	r.containerID = containerID
	return nil
}

// resolveBaseImage determines the base image for single-container configs.
func (r *UnifiedRuntime) resolveBaseImage(ctx context.Context, opts UpOptions) (string, error) {
	switch plan := r.resolved.Plan.(type) {
	case *devcontainer.ImagePlan:
		fmt.Printf("Using image: %s\n", plan.Image)

		exists, err := MustDocker().ImageExists(ctx, plan.Image)
		if err != nil {
			return "", fmt.Errorf("failed to check image: %w", err)
		}

		if !exists || opts.Pull {
			fmt.Printf("Pulling image: %s\n", plan.Image)
			if err := MustDocker().PullImageWithProgress(ctx, plan.Image, os.Stdout); err != nil {
				return "", fmt.Errorf("failed to pull image: %w", err)
			}
		}

		return plan.Image, nil

	case *devcontainer.DockerfilePlan:
		imageTag := fmt.Sprintf("%s%s:%s", common.ImageTagPrefix, r.resolved.ID, r.resolved.Hashes.Overall[:common.HashTruncationLength])
		fmt.Printf("Building image: %s\n", imageTag)

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

// buildDerivedImage builds an image with features installed using the CLI.
func (r *UnifiedRuntime) buildDerivedImage(ctx context.Context, baseImage string, rebuild bool) (string, error) {
	// Get derived image tag (use temp tag if stable tag unavailable)
	derivedTag := r.getDerivedImageTag()
	if derivedTag == "" {
		derivedTag = fmt.Sprintf("dcx-derived-temp:%d", time.Now().UnixNano())
	}

	// Get base image metadata for merging
	baseImageMetadata := ""
	if cliBuilder, ok := r.builder.(*build.CLIBuilder); ok {
		labels, err := cliBuilder.GetImageLabels(ctx, baseImage)
		if err == nil && labels != nil {
			baseImageMetadata = labels[devcontainer.DevcontainerMetadataLabel]
		}
	}

	// Build the derived image using the CLI builder
	remoteUser := r.resolved.RemoteUser
	containerUser := r.resolved.ContainerUser

	derivedImage, err := r.builder.BuildWithFeatures(ctx, build.FeatureBuildOptions{
		BaseImage:         baseImage,
		Tag:               derivedTag,
		Features:          r.resolved.Features,
		RemoteUser:        remoteUser,
		ContainerUser:     containerUser,
		Rebuild:           rebuild,
		Progress:          os.Stdout,
		BaseImageMetadata: baseImageMetadata,
		LocalConfig:       r.resolved.RawConfig,
	})
	if err != nil {
		return "", fmt.Errorf("failed to build derived image: %w", err)
	}

	// Apply UID update layer if needed
	finalImage, err := r.applyUIDUpdateLayer(ctx, derivedImage, rebuild)
	if err != nil {
		return "", err
	}

	return finalImage, nil
}

// applyUIDUpdateLayer applies a UID update layer to match host UID/GID using the SDK.
func (r *UnifiedRuntime) applyUIDUpdateLayer(ctx context.Context, baseImage string, rebuild bool) (string, error) {
	if !r.resolved.ShouldUpdateUID {
		return baseImage, nil
	}

	effectiveUser := r.resolved.EffectiveUser
	hostUID := r.resolved.HostUID
	hostGID := r.resolved.HostGID

	uidTag := fmt.Sprintf("%s-uid%d", baseImage, hostUID)

	imageUser := r.resolved.ContainerUser
	if imageUser == "" {
		imageUser = effectiveUser
	}

	finalImage, err := r.builder.BuildUIDUpdate(ctx, build.UIDBuildOptions{
		BaseImage:  baseImage,
		Tag:        uidTag,
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

	return finalImage, nil
}

// createContainer creates a single container.
func (r *UnifiedRuntime) createContainer(ctx context.Context, imageRef string) (string, error) {
	containerName := r.resolved.ServiceName
	workspaceFolder := r.resolved.WorkspaceFolder

	containerLabels := r.buildLabels()
	mountColl := r.buildMounts()
	env := r.buildEnvironment()

	// Build workspace mount as structured type
	var workspaceMount *devcontainer.Mount
	if r.resolved.WorkspaceMount != "" {
		// Parse the workspace mount string
		parsed := devcontainer.ParseWorkspaceMount(r.resolved.WorkspaceMount)
		if parsed != nil {
			workspaceMount = parsed
		}
	}
	if workspaceMount == nil && r.resolved.LocalRoot != "" && workspaceFolder != "" {
		// Default workspace mount
		workspaceMount = &devcontainer.Mount{
			Type:   "bind",
			Source: r.resolved.LocalRoot,
			Target: workspaceFolder,
		}
	}

	ports := r.buildPortBindings()

	createOpts := CreateContainerOptions{
		Name:            containerName,
		Image:           imageRef,
		Labels:          containerLabels,
		Env:             env,
		WorkspacePath:   r.resolved.LocalRoot,
		WorkspaceFolder: workspaceFolder,
		WorkspaceMount:  workspaceMount,
		Mounts:          mountColl.Mounts,
		Tmpfs:           mountColl.Tmpfs,
		Ports:           ports,
		CapAdd:          r.resolved.CapAdd,
		SecurityOpt:     r.resolved.SecurityOpt,
		Privileged:      r.resolved.Privileged,
		Init:            r.resolved.Init,
		User:            r.resolved.ContainerUser,
	}

	// Apply feature security requirements (capabilities, security options, privileged mode)
	if len(r.resolved.Features) > 0 {
		reqs := features.GetSecurityRequirements(r.resolved.Features)

		// Warn user about elevated permissions
		if reqs.Privileged || len(reqs.Capabilities) > 0 {
			ui.Warning("Features require elevated permissions:")
			for _, name := range reqs.FeatureNames {
				ui.Warning("  - %s", name)
			}
		}

		// Apply feature requirements to container
		createOpts.CapAdd = append(createOpts.CapAdd, reqs.Capabilities...)
		createOpts.SecurityOpt = append(createOpts.SecurityOpt, reqs.SecurityOpts...)
		if reqs.Privileged {
			createOpts.Privileged = true
		}

		// Check if any feature needs init
		if features.NeedsInit(r.resolved.Features) {
			createOpts.Init = true
		}

		// Collect feature environment variables
		featureEnv := features.CollectContainerEnv(r.resolved.Features)
		for k, v := range featureEnv {
			createOpts.Env = append(createOpts.Env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	// Pass GPU requirements to container creation
	if r.resolved.GPURequirements != nil && r.resolved.GPURequirements.Enabled {
		if r.resolved.GPURequirements.Count > 0 {
			createOpts.GPURequest = strconv.Itoa(r.resolved.GPURequirements.Count)
		} else {
			createOpts.GPURequest = "all"
		}
	}

	// Apply parsed runArgs from devcontainer.json
	if r.resolved.RunArgs != nil {
		runArgs := r.resolved.RunArgs

		// Apply parsed values (runArgs override defaults)
		if runArgs.NetworkMode != "" {
			createOpts.NetworkMode = runArgs.NetworkMode
		}
		if runArgs.IpcMode != "" {
			createOpts.IpcMode = runArgs.IpcMode
		}
		if runArgs.PidMode != "" {
			createOpts.PidMode = runArgs.PidMode
		}
		if runArgs.ShmSize > 0 {
			createOpts.ShmSize = runArgs.ShmSize
		}
		if runArgs.User != "" {
			createOpts.User = runArgs.User
		}
		createOpts.CapDrop = append(createOpts.CapDrop, runArgs.CapDrop...)
		createOpts.Devices = append(createOpts.Devices, runArgs.Devices...)
		createOpts.ExtraHosts = append(createOpts.ExtraHosts, runArgs.ExtraHosts...)
		for k, v := range runArgs.Sysctls {
			if createOpts.Sysctls == nil {
				createOpts.Sysctls = make(map[string]string)
			}
			createOpts.Sysctls[k] = v
		}
	}

	// Handle overrideCommand
	// Per spec: default true for image/dockerfile, false for compose
	shouldOverride := false
	if r.resolved.RawConfig != nil && r.resolved.RawConfig.OverrideCommand != nil {
		// Explicit setting takes precedence
		shouldOverride = *r.resolved.RawConfig.OverrideCommand
	} else {
		// Default: true for image/dockerfile, false for compose
		_, isCompose := r.resolved.Plan.(*devcontainer.ComposePlan)
		shouldOverride = !isCompose
	}
	if shouldOverride {
		createOpts.Entrypoint = []string{"sleep"}
		createOpts.Cmd = []string{"infinity"}
	}

	return MustDocker().CreateContainer(ctx, createOpts)
}

// buildLabels builds the container labels.
func (r *UnifiedRuntime) buildLabels() map[string]string {
	l := state.NewContainerLabels()
	l.WorkspaceID = r.resolved.ID
	l.WorkspaceName = r.resolved.Name
	l.WorkspacePath = r.resolved.LocalRoot
	l.ConfigPath = r.resolved.ConfigPath
	l.HashConfig = r.resolved.Hashes.Config
	l.HashDockerfile = r.resolved.Hashes.Dockerfile
	l.HashCompose = r.resolved.Hashes.Compose
	l.HashFeatures = r.resolved.Hashes.Features
	l.HashOverall = r.resolved.Hashes.Overall
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

// mountCollections holds separated mount types for container creation.
type mountCollections struct {
	Mounts []devcontainer.Mount // Structured mounts
	Tmpfs  map[string]string    // For tmpfs mounts
}

// buildMounts builds the container mounts, separating tmpfs from other mounts.
func (r *UnifiedRuntime) buildMounts() mountCollections {
	result := mountCollections{
		Tmpfs: make(map[string]string),
	}

	// Add tmpfs for /run/secrets only when runtime secrets are configured.
	// This ensures secrets are stored in memory and not persisted to the container's writable layer.
	if len(r.resolved.RuntimeSecrets) > 0 {
		result.Tmpfs[common.SecretsDir] = "rw,noexec,nosuid,size=1m"
	}

	for _, m := range r.resolved.Mounts {
		if m.Type == "tmpfs" {
			// Tmpfs handled separately via HostConfig.Tmpfs
			result.Tmpfs[m.Target] = ""
		} else {
			// Pass structured mount directly
			result.Mounts = append(result.Mounts, m)
		}
	}
	return result
}

// buildEnvironment builds the container environment.
func (r *UnifiedRuntime) buildEnvironment() []string {
	var env []string
	for k, v := range r.resolved.ContainerEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

// buildPortBindings combines forwardPorts and appPorts into a single slice.
// AppPorts are bound to localhost for security per the devcontainer spec.
func (r *UnifiedRuntime) buildPortBindings() []devcontainer.PortForward {
	var ports []devcontainer.PortForward

	// Add forward ports (bind to all interfaces by default)
	ports = append(ports, r.resolved.ForwardPorts...)

	// Add app ports (bound to localhost for security)
	for _, ap := range r.resolved.AppPorts {
		ap.Host = "localhost"
		ports = append(ports, ap)
	}

	return ports
}

// Start implements ContainerRuntime.Start.
func (r *UnifiedRuntime) Start(ctx context.Context) error {
	if r.resolved != nil {
		if _, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok {
			plan := r.resolved.Plan.(*devcontainer.ComposePlan)
			args := r.composeBaseArgs(plan)
			args = append(args, "start")
			return r.runCompose(ctx, args)
		}
	}

	// Lightweight compose runtime - use Compose client
	if r.compose != nil {
		return r.compose.Start(ctx)
	}

	// Single container
	return MustDocker().StartContainer(ctx, r.containerName)
}

// Stop implements ContainerRuntime.Stop.
func (r *UnifiedRuntime) Stop(ctx context.Context) error {
	if r.resolved != nil {
		if plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok {
			args := r.composeBaseArgs(plan)
			args = append(args, "stop")
			return r.runCompose(ctx, args)
		}
	}

	// Lightweight compose runtime - use Compose client
	if r.compose != nil {
		return r.compose.Stop(ctx)
	}

	// Single container
	return MustDocker().StopContainer(ctx, r.containerName, nil)
}

// Down implements ContainerRuntime.Down.
func (r *UnifiedRuntime) Down(ctx context.Context, opts DownOptions) error {
	if r.resolved != nil {
		if plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok {
			args := r.composeBaseArgs(plan)
			args = append(args, "down")
			if opts.RemoveVolumes {
				args = append(args, "-v")
			}
			if opts.RemoveOrphans {
				args = append(args, "--remove-orphans")
			}
			return r.runCompose(ctx, args)
		}
	}

	// Lightweight compose runtime - use Compose client
	if r.compose != nil {
		return r.compose.Down(ctx, ComposeDownOptions(opts))
	}

	// Single container
	return MustDocker().RemoveContainer(ctx, r.containerName, true, opts.RemoveVolumes)
}

// Build implements ContainerRuntime.Build.
func (r *UnifiedRuntime) Build(ctx context.Context, opts BuildOptions) error {
	if r.resolved == nil {
		return fmt.Errorf("no resolved configuration - use NewUnifiedRuntime")
	}

	if plan, ok := r.resolved.Plan.(*devcontainer.ComposePlan); ok {
		args := r.composeBaseArgs(plan)
		args = append(args, "build")
		if opts.NoCache {
			args = append(args, "--no-cache")
		}
		if opts.Pull {
			args = append(args, "--pull")
		}
		return r.runCompose(ctx, args)
	}

	// Single container - build image
	upOpts := UpOptions{Build: true, Rebuild: opts.NoCache, Pull: opts.Pull}
	_, err := r.resolveBaseImage(ctx, upOpts)
	return err
}

// Compose helper methods

func (r *UnifiedRuntime) composeBaseArgs(plan *devcontainer.ComposePlan) []string {
	projectName := r.containerName
	if plan != nil && plan.ProjectName != "" {
		projectName = plan.ProjectName
	} else if r.composeProject != "" {
		projectName = r.composeProject
	}
	args := []string{"-p", projectName}

	if plan != nil {
		for _, f := range plan.Files {
			args = append(args, "-f", f)
		}
	}

	if r.overridePath != "" {
		args = append(args, "-f", r.overridePath)
	}

	return args
}

func (r *UnifiedRuntime) runCompose(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	if r.resolved != nil {
		cmd.Dir = r.resolved.ConfigDir
	} else if r.workspacePath != "" {
		cmd.Dir = r.workspacePath
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func (r *UnifiedRuntime) generateComposeOverride(plan *devcontainer.ComposePlan, buildSecrets map[string]string) (string, error) {
	var sb strings.Builder
	sb.WriteString("# Generated by dcx - do not edit\n")
	sb.WriteString("services:\n")
	sb.WriteString(fmt.Sprintf("  %s:\n", plan.Service))

	// Add labels
	sb.WriteString("    labels:\n")
	for k, v := range r.buildLabels() {
		sb.WriteString(fmt.Sprintf("      %s: %q\n", k, v))
	}

	// Add derived image if features were installed
	if r.derivedImage != "" {
		sb.WriteString(fmt.Sprintf("    image: %s\n", r.derivedImage))
	}

	// Add build secrets if any (for compose builds without features)
	if len(buildSecrets) > 0 && r.derivedImage == "" {
		sb.WriteString("    build:\n")
		sb.WriteString("      secrets:\n")
		for name := range buildSecrets {
			sb.WriteString(fmt.Sprintf("        - %s\n", name))
		}
	}

	// Add forwardPorts
	if len(r.resolved.ForwardPorts) > 0 {
		sb.WriteString("    ports:\n")
		for _, port := range r.resolved.ForwardPorts {
			if port.HostPort == port.ContainerPort {
				sb.WriteString(fmt.Sprintf("      - \"%d\"\n", port.ContainerPort))
			} else {
				sb.WriteString(fmt.Sprintf("      - \"%d:%d\"\n", port.HostPort, port.ContainerPort))
			}
		}
	}

	// Add mounts
	mountColl := r.buildMounts()
	if len(mountColl.Mounts) > 0 {
		sb.WriteString("    volumes:\n")
		for _, m := range mountColl.Mounts {
			// Convert structured mount back to compose volume string
			mountStr := fmt.Sprintf("%s:%s", m.Source, m.Target)
			if m.ReadOnly {
				mountStr += ":ro"
			}
			sb.WriteString(fmt.Sprintf("      - %q\n", mountStr))
		}
	}

	// Add tmpfs mounts
	if len(mountColl.Tmpfs) > 0 {
		sb.WriteString("    tmpfs:\n")
		for path, opts := range mountColl.Tmpfs {
			if opts != "" {
				sb.WriteString(fmt.Sprintf("      - %q\n", path+":"+opts))
			} else {
				sb.WriteString(fmt.Sprintf("      - %q\n", path))
			}
		}
	}

	// Add top-level secrets definitions if any
	if len(buildSecrets) > 0 && r.derivedImage == "" {
		sb.WriteString("secrets:\n")
		for name, path := range buildSecrets {
			sb.WriteString(fmt.Sprintf("  %s:\n", name))
			sb.WriteString(fmt.Sprintf("    file: %s\n", path))
		}
	}

	return sb.String(), nil
}

func (r *UnifiedRuntime) ensureServicesBuilt(ctx context.Context, plan *devcontainer.ComposePlan, buildSecrets map[string]string) error {
	args := r.composeBaseArgs(plan)

	// Add build secrets override if any
	if len(buildSecrets) > 0 {
		override := r.generateBuildSecretsOverride(plan, buildSecrets)
		overridePath, err := r.writeToTempFile(override, "dcx-build-secrets-*.yml")
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(overridePath) }()
		args = append(args, "-f", overridePath)
	}

	args = append(args, "build")
	return r.runCompose(ctx, args)
}

// generateBuildSecretsOverride generates a compose override file with build secrets.
// Secrets are referenced by their temp file paths.
func (r *UnifiedRuntime) generateBuildSecretsOverride(plan *devcontainer.ComposePlan, buildSecrets map[string]string) string {
	var sb strings.Builder
	sb.WriteString("# Generated by dcx - build secrets override\n")
	sb.WriteString("services:\n")
	sb.WriteString(fmt.Sprintf("  %s:\n", plan.Service))
	sb.WriteString("    build:\n")
	sb.WriteString("      secrets:\n")
	for name := range buildSecrets {
		sb.WriteString(fmt.Sprintf("        - %s\n", name))
	}
	sb.WriteString("secrets:\n")
	for name, path := range buildSecrets {
		sb.WriteString(fmt.Sprintf("  %s:\n", name))
		sb.WriteString(fmt.Sprintf("    file: %s\n", path))
	}
	return sb.String()
}

func (r *UnifiedRuntime) buildDerivedImageForCompose(ctx context.Context, opts UpOptions, plan *devcontainer.ComposePlan) error {
	baseImage, err := r.getComposeBaseImage(ctx, plan)
	if err != nil {
		return fmt.Errorf("failed to determine base image: %w", err)
	}

	derivedImage, err := r.buildDerivedImage(ctx, baseImage, opts.Rebuild)
	if err != nil {
		return err
	}

	r.derivedImage = derivedImage
	return nil
}

func (r *UnifiedRuntime) applyUIDUpdateForCompose(ctx context.Context, opts UpOptions, plan *devcontainer.ComposePlan) error {
	if !r.resolved.ShouldUpdateUID {
		return nil
	}

	baseImage, err := r.getComposeBaseImage(ctx, plan)
	if err != nil {
		return fmt.Errorf("failed to determine base image: %w", err)
	}

	uidImage, err := r.applyUIDUpdateLayer(ctx, baseImage, opts.Rebuild)
	if err != nil {
		return err
	}

	if uidImage != baseImage {
		r.derivedImage = uidImage
	}

	return nil
}

func (r *UnifiedRuntime) getComposeBaseImage(ctx context.Context, plan *devcontainer.ComposePlan) (string, error) {
	if r.resolved.BaseImage != "" {
		return r.resolved.BaseImage, nil
	}

	if plan == nil {
		return "", fmt.Errorf("no compose configuration found")
	}

	paths := plan.Files
	if len(paths) == 0 {
		return "", fmt.Errorf("no compose files specified")
	}

	// Use docker compose config to get fully resolved configuration
	args := []string{"compose"}
	for _, f := range paths {
		args = append(args, "-f", f)
	}
	args = append(args, "config", "--format", "json")

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = filepath.Dir(paths[0])
	output, err := cmd.Output()
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

// getDerivedImageTag returns the expected tag for the derived image.
// This mirrors the logic in buildDerivedImage but without building.
func (r *UnifiedRuntime) getDerivedImageTag() string {
	if r.resolved.DerivedImage != "" {
		return r.resolved.DerivedImage
	}
	if r.resolved.ID != "" && r.resolved.Hashes != nil && r.resolved.Hashes.Config != "" && len(r.resolved.Hashes.Config) >= common.HashTruncationLength {
		return fmt.Sprintf("%s%s:%s-features", common.ImageTagPrefix, r.resolved.ID, r.resolved.Hashes.Config[:common.HashTruncationLength])
	}
	if r.resolved.ID != "" {
		return fmt.Sprintf("dcx-derived-%s:latest", r.resolved.ID)
	}
	// Fallback - can't cache without stable tag
	return ""
}

// derivedImageExists checks if the derived image already exists locally.
func (r *UnifiedRuntime) derivedImageExists(ctx context.Context, tag string) bool {
	if tag == "" {
		return false
	}
	exists, err := MustDocker().ImageExists(ctx, tag)
	return err == nil && exists
}

func (r *UnifiedRuntime) writeToTempFile(content, pattern string) (string, error) {
	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	_ = tmpFile.Close()
	return tmpFile.Name(), nil
}

// Ensure UnifiedRuntime implements ContainerRuntime.
var _ ContainerRuntime = (*UnifiedRuntime)(nil)
