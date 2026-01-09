// Package runner provides the unified devcontainer runner.
package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	composecli "github.com/compose-spec/compose-go/v2/cli"
	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/workspace"
)

// UnifiedRunner manages devcontainer operations using a compose-first approach.
// It handles both single-container and compose-based configurations by
// generating ephemeral compose projects for single containers.
type UnifiedRunner struct {
	workspace *workspace.Workspace
	docker    *docker.Client

	// Runtime state
	overridePath     string
	derivedImage     string
	resolvedFeatures []*features.Feature
}

// NewUnifiedRunner creates a new unified runner for a workspace.
func NewUnifiedRunner(ws *workspace.Workspace, dockerClient *docker.Client) (*UnifiedRunner, error) {
	if ws == nil {
		return nil, fmt.Errorf("workspace is required")
	}
	if dockerClient == nil {
		return nil, fmt.Errorf("docker client is required")
	}

	return &UnifiedRunner{
		workspace: ws,
		docker:    dockerClient,
	}, nil
}

// NewUnifiedRunnerForExisting creates a lightweight runner for operating on existing environments.
// This is used for simple operations (stop, start, restart, down) where we don't need a full workspace.
// The projectName is the compose project name, envKey is the workspace ID.
func NewUnifiedRunnerForExisting(workspacePath, projectName, envKey string) *UnifiedRunner {
	// Create a minimal workspace for compose operations
	ws := &workspace.Workspace{
		LocalRoot: workspacePath,
		ID:        envKey,
		Resolved: &workspace.ResolvedConfig{
			PlanType:    workspace.PlanTypeCompose,
			ServiceName: projectName,
			Compose: &workspace.ComposePlan{
				ProjectName: projectName,
			},
		},
	}

	return &UnifiedRunner{
		workspace: ws,
		// docker client is nil - operations will use compose CLI
	}
}

// Up starts the devcontainer environment.
func (r *UnifiedRunner) Up(ctx context.Context, opts UpOptions) error {
	ws := r.workspace

	// Check if we need to handle features (from raw config)
	hasFeatures := ws.RawConfig != nil && len(ws.RawConfig.Features) > 0

	// Determine the approach based on plan type
	switch ws.Resolved.PlanType {
	case workspace.PlanTypeCompose:
		return r.upCompose(ctx, opts, hasFeatures)
	case workspace.PlanTypeImage, workspace.PlanTypeDockerfile:
		return r.upSingle(ctx, opts, hasFeatures)
	default:
		return fmt.Errorf("unsupported plan type: %s", ws.Resolved.PlanType)
	}
}

// upCompose handles compose-based configurations.
func (r *UnifiedRunner) upCompose(ctx context.Context, opts UpOptions, hasFeatures bool) error {
	ws := r.workspace

	// Ensure services with build configs are built first if needed
	if hasFeatures || opts.Rebuild {
		if err := r.ensureServicesBuilt(ctx); err != nil {
			return fmt.Errorf("failed to build services: %w", err)
		}
	}

	// Build derived image with features if needed
	if hasFeatures {
		if err := r.buildDerivedImageForCompose(ctx, opts); err != nil {
			return fmt.Errorf("failed to build derived image with features: %w", err)
		}
	} else {
		// Even without features, we may need to apply UID update layer for compose
		if err := r.applyUIDUpdateForCompose(ctx, opts); err != nil {
			return fmt.Errorf("failed to apply UID update: %w", err)
		}
	}

	// Generate override file
	override, err := r.generateComposeOverride()
	if err != nil {
		return fmt.Errorf("failed to generate override: %w", err)
	}

	// Write override to temp file
	r.overridePath, err = r.writeToTempFile(override, "dcx-override-*.yml")
	if err != nil {
		return err
	}
	defer os.Remove(r.overridePath)

	// Build compose args
	args := r.composeBaseArgs()
	args = append(args, "up", "-d")

	// Add --build only if explicitly requested and we DON'T have features
	if opts.Build && !hasFeatures {
		args = append(args, "--build")
	}

	// Determine which services to start
	if ws.Resolved.Compose != nil && len(ws.Resolved.Compose.RunServices) > 0 {
		args = append(args, ws.Resolved.Compose.RunServices...)
	}

	return r.runCompose(ctx, args)
}

// upSingle handles single-container configurations (image or Dockerfile).
func (r *UnifiedRunner) upSingle(ctx context.Context, opts UpOptions, hasFeatures bool) error {
	// Resolve the base image
	baseImage, err := r.resolveBaseImage(ctx, opts)
	if err != nil {
		return err
	}

	// Build derived image with features if needed
	finalImage := baseImage
	if hasFeatures {
		derivedImage, err := r.buildDerivedImage(ctx, baseImage, opts.Rebuild, opts.Pull)
		if err != nil {
			return fmt.Errorf("failed to build derived image with features: %w", err)
		}
		finalImage = derivedImage
		r.derivedImage = derivedImage
	} else {
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
	if err := r.docker.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// resolveBaseImage determines the base image for single-container configs.
func (r *UnifiedRunner) resolveBaseImage(ctx context.Context, opts UpOptions) (string, error) {
	ws := r.workspace

	if ws.Resolved.Image != "" {
		// Image-based configuration
		fmt.Printf("Using image: %s\n", ws.Resolved.Image)

		exists, err := r.docker.ImageExists(ctx, ws.Resolved.Image)
		if err != nil {
			return "", fmt.Errorf("failed to check image: %w", err)
		}

		if !exists || opts.Build {
			fmt.Printf("Pulling image: %s\n", ws.Resolved.Image)
			if err := r.docker.PullImageWithProgress(ctx, ws.Resolved.Image, os.Stdout); err != nil {
				return "", fmt.Errorf("failed to pull image: %w", err)
			}
		}

		return ws.Resolved.Image, nil
	}

	if ws.Resolved.Dockerfile != nil {
		// Dockerfile-based configuration
		imageTag := fmt.Sprintf("dcx/%s:%s", ws.ID, ws.Hashes.Overall[:12])
		fmt.Printf("Building image: %s\n", imageTag)

		if err := r.buildDockerfile(ctx, imageTag); err != nil {
			return "", fmt.Errorf("failed to build image: %w", err)
		}

		return imageTag, nil
	}

	return "", fmt.Errorf("no image or build configuration found")
}

// buildDockerfile builds an image from a Dockerfile.
func (r *UnifiedRunner) buildDockerfile(ctx context.Context, imageTag string) error {
	ws := r.workspace
	df := ws.Resolved.Dockerfile

	buildCtx := df.Context
	if buildCtx == "" {
		buildCtx = ws.ConfigDir
	}

	buildArgs := make(map[string]string)
	for k, v := range df.Args {
		buildArgs[k] = v
	}

	return r.docker.BuildImage(ctx, docker.BuildOptions{
		Tag:        imageTag,
		Dockerfile: df.Path,
		Context:    buildCtx,
		Args:       buildArgs,
		Target:     df.Target,
		ConfigDir:  ws.ConfigDir,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	})
}

// buildDerivedImage builds an image with features installed.
func (r *UnifiedRunner) buildDerivedImage(ctx context.Context, baseImage string, rebuild, _ bool) (string, error) {
	ws := r.workspace

	// Use pre-computed derived image tag from workspace
	var derivedTag string
	if ws.Build != nil && ws.Build.DerivedImage != "" {
		derivedTag = ws.Build.DerivedImage
	} else if ws.ID != "" && ws.Hashes != nil && ws.Hashes.Config != "" && len(ws.Hashes.Config) >= 12 {
		// Fallback: compute if not pre-computed
		derivedTag = fmt.Sprintf("dcx/%s:%s-features", ws.ID, ws.Hashes.Config[:12])
	} else {
		// Last resort fallback
		derivedTag = fmt.Sprintf("dcx-derived-%s:latest", ws.ID)
		if ws.ID == "" {
			derivedTag = fmt.Sprintf("dcx-derived-temp:%d", time.Now().UnixNano())
		}
	}

	// Use pre-resolved features from workspace (populated by builder)
	// Feature mounts are also pre-merged into ws.Resolved.Mounts by the builder
	r.resolvedFeatures = ws.ResolvedFeatures

	// Check if derived image already exists and is up-to-date
	if !rebuild {
		exists, err := r.docker.ImageExists(ctx, derivedTag)
		if err == nil && exists {
			fmt.Printf("Using cached derived image: %s\n", derivedTag)
			// Still need to apply UID update layer (may be cached too)
			return r.applyUIDUpdateLayer(ctx, derivedTag, rebuild)
		}
	}

	fmt.Printf("Building derived image: %s\n", derivedTag)

	// Build the derived image using features manager
	featureMgr, err := features.NewManager(ws.ConfigDir)
	if err != nil {
		return "", fmt.Errorf("failed to create feature manager: %w", err)
	}

	remoteUser := ws.Resolved.RemoteUser
	containerUser := ws.Resolved.ContainerUser
	if err := featureMgr.BuildDerivedImage(ctx, baseImage, derivedTag, r.resolvedFeatures, ws.ConfigDir, remoteUser, containerUser); err != nil {
		return "", fmt.Errorf("failed to build derived image: %w", err)
	}

	// Apply UID update layer if needed (after features, per devcontainer spec)
	finalImage, err := r.applyUIDUpdateLayer(ctx, derivedTag, rebuild)
	if err != nil {
		return "", err
	}

	return finalImage, nil
}

// applyUIDUpdateLayer applies a UID update layer to match host UID/GID.
// This is done at build time per the devcontainer spec.
// Returns the final image tag (may be same as input if no update needed).
func (r *UnifiedRunner) applyUIDUpdateLayer(ctx context.Context, baseImage string, rebuild bool) (string, error) {
	ws := r.workspace

	// Use pre-computed decision from workspace builder
	if !ws.Build.ShouldUpdateUID {
		return baseImage, nil
	}

	// Use pre-computed values from workspace
	effectiveUser := ws.Resolved.EffectiveUser
	hostUID := ws.Resolved.HostUID
	hostGID := ws.Resolved.HostGID

	// Build UID update image tag
	uidTag := fmt.Sprintf("%s-uid%d", baseImage, hostUID)

	// Check if UID update image already exists
	if !rebuild {
		exists, err := r.docker.ImageExists(ctx, uidTag)
		if err == nil && exists {
			fmt.Printf("Using cached UID-updated image: %s\n", uidTag)
			return uidTag, nil
		}
	}

	fmt.Printf("Updating UID/GID to %d:%d for user %s...\n", hostUID, hostGID, effectiveUser)

	// Determine the user the container should run as after UID update
	imageUser := ws.Resolved.ContainerUser
	if imageUser == "" {
		imageUser = effectiveUser
	}

	if err := features.BuildUpdateUIDImage(ctx, baseImage, uidTag, effectiveUser, imageUser, hostUID, hostGID); err != nil {
		return "", fmt.Errorf("failed to build UID update image: %w", err)
	}

	return uidTag, nil
}

// createContainer creates a single container.
func (r *UnifiedRunner) createContainer(ctx context.Context, imageRef string) (string, error) {
	ws := r.workspace

	containerName := ws.Resolved.ServiceName
	workspaceFolder := ws.Resolved.WorkspaceFolder

	// Build container labels
	containerLabels := r.buildLabels()

	// Build mounts
	mounts := r.buildMounts()

	// Build environment
	env := r.buildEnvironment()

	// Use custom workspaceMount if provided, otherwise build default
	workspaceMount := ws.Resolved.WorkspaceMount
	if workspaceMount == "" {
		workspaceMount = fmt.Sprintf("type=bind,source=%s,target=%s", ws.LocalRoot, workspaceFolder)
	}

	// Build port bindings
	ports := r.buildPortBindings()

	// Create container config
	createOpts := docker.CreateContainerOptions{
		Name:            containerName,
		Image:           imageRef,
		Labels:          containerLabels,
		Env:             env,
		WorkspacePath:   ws.LocalRoot,
		WorkspaceFolder: workspaceFolder,
		WorkspaceMount:  workspaceMount,
		Mounts:          mounts,
		Ports:           ports,
		CapAdd:         ws.Resolved.CapAdd,
		SecurityOpt:    ws.Resolved.SecurityOpt,
		Privileged:     ws.Resolved.Privileged,
		Init:           ws.Resolved.Init,
		User:           ws.Resolved.ContainerUser,
	}

	// Handle overrideCommand
	if ws.RawConfig != nil && ws.RawConfig.OverrideCommand != nil && *ws.RawConfig.OverrideCommand {
		createOpts.Entrypoint = []string{"sleep"}
		createOpts.Cmd = []string{"infinity"}
	}

	return r.docker.CreateContainer(ctx, createOpts)
}

// buildLabels builds the container labels.
func (r *UnifiedRunner) buildLabels() map[string]string {
	ws := r.workspace

	l := labels.NewLabels()
	l.WorkspaceID = ws.ID
	l.WorkspaceName = ws.Name
	l.WorkspacePath = ws.LocalRoot
	l.ConfigPath = ws.ConfigPath
	l.HashConfig = ws.Hashes.Config
	l.HashDockerfile = ws.Hashes.Dockerfile
	l.HashCompose = ws.Hashes.Compose
	l.HashFeatures = ws.Hashes.Features
	l.HashOverall = ws.Hashes.Overall
	l.BuildMethod = string(ws.Resolved.PlanType)
	l.IsPrimary = true

	if ws.Resolved.Image != "" {
		l.BaseImage = ws.Resolved.Image
	}
	if r.derivedImage != "" {
		l.DerivedImage = r.derivedImage
	}

	// Set compose-specific labels
	if ws.Resolved.Compose != nil {
		l.ComposeProject = ws.Resolved.Compose.ProjectName
		l.ComposeService = ws.Resolved.Compose.Service
	}

	// Store installed features
	if len(r.resolvedFeatures) > 0 {
		featureIDs := make([]string, len(r.resolvedFeatures))
		for i, f := range r.resolvedFeatures {
			featureIDs[i] = f.ID
		}
		l.FeaturesInstalled = featureIDs
	}

	return l.ToMap()
}

// buildMounts builds the container mounts as strings in Docker bind format (source:target[:ro]).
func (r *UnifiedRunner) buildMounts() []string {
	ws := r.workspace

	var mounts []string
	for _, m := range ws.Resolved.Mounts {
		mountStr := fmt.Sprintf("%s:%s", m.Source, m.Target)
		if m.ReadOnly {
			mountStr += ":ro"
		}
		mounts = append(mounts, mountStr)
	}
	return mounts
}

// buildEnvironment builds the container environment.
func (r *UnifiedRunner) buildEnvironment() []string {
	ws := r.workspace

	var env []string
	for k, v := range ws.Resolved.ContainerEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

// buildPortBindings builds port bindings from forward ports.
func (r *UnifiedRunner) buildPortBindings() []string {
	ws := r.workspace
	if len(ws.Resolved.ForwardPorts) == 0 {
		return nil
	}

	ports := make([]string, 0, len(ws.Resolved.ForwardPorts))
	for _, p := range ws.Resolved.ForwardPorts {
		if p.HostPort == p.ContainerPort || p.HostPort == 0 {
			ports = append(ports, fmt.Sprintf("%d", p.ContainerPort))
		} else {
			ports = append(ports, fmt.Sprintf("%d:%d", p.HostPort, p.ContainerPort))
		}
	}
	return ports
}

// Start starts an existing stopped environment.
func (r *UnifiedRunner) Start(ctx context.Context) error {
	ws := r.workspace

	if ws.Resolved.PlanType == workspace.PlanTypeCompose {
		args := r.composeBaseArgs()
		args = append(args, "start")
		return r.runCompose(ctx, args)
	}

	// Single container
	containerName := ws.Resolved.ServiceName
	return r.docker.StartContainer(ctx, containerName)
}

// Stop stops a running environment.
func (r *UnifiedRunner) Stop(ctx context.Context) error {
	ws := r.workspace

	if ws.Resolved.PlanType == workspace.PlanTypeCompose {
		args := r.composeBaseArgs()
		args = append(args, "stop")
		return r.runCompose(ctx, args)
	}

	// Single container
	containerName := ws.Resolved.ServiceName
	return r.docker.StopContainer(ctx, containerName, nil)
}

// Restart restarts a running environment.
func (r *UnifiedRunner) Restart(ctx context.Context) error {
	ws := r.workspace

	if ws.Resolved.PlanType == workspace.PlanTypeCompose {
		args := r.composeBaseArgs()
		args = append(args, "restart")
		return r.runCompose(ctx, args)
	}

	// Single container - stop then start
	containerName := ws.Resolved.ServiceName
	if r.docker != nil {
		if err := r.docker.StopContainer(ctx, containerName, nil); err != nil {
			return fmt.Errorf("failed to stop container: %w", err)
		}
		return r.docker.StartContainer(ctx, containerName)
	}
	return fmt.Errorf("docker client required for single container restart")
}

// Down removes the environment.
func (r *UnifiedRunner) Down(ctx context.Context, opts DownOptions) error {
	ws := r.workspace

	if ws.Resolved.PlanType == workspace.PlanTypeCompose {
		args := r.composeBaseArgs()
		args = append(args, "down")
		if opts.RemoveVolumes {
			args = append(args, "-v")
		}
		if opts.RemoveOrphans {
			args = append(args, "--remove-orphans")
		}
		return r.runCompose(ctx, args)
	}

	// Single container
	containerName := ws.Resolved.ServiceName
	return r.docker.RemoveContainer(ctx, containerName, true, opts.RemoveVolumes)
}

// Build builds the environment images.
func (r *UnifiedRunner) Build(ctx context.Context, opts BuildOptions) error {
	ws := r.workspace

	if ws.Resolved.PlanType == workspace.PlanTypeCompose {
		args := r.composeBaseArgs()
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

// Exec executes a command in the running environment.
func (r *UnifiedRunner) Exec(ctx context.Context, cmd []string, opts ExecOptions) (int, error) {
	ws := r.workspace

	containerName := ws.Resolved.ServiceName
	workingDir := opts.WorkingDir
	if workingDir == "" {
		workingDir = ws.Resolved.WorkspaceFolder
	}

	user := opts.User
	if user == "" {
		user = ws.Resolved.RemoteUser
	}

	execConfig := docker.ExecConfig{
		Cmd:        cmd,
		WorkingDir: workingDir,
		User:       user,
		Env:        opts.Env,
		Stdin:      opts.Stdin,
		Stdout:     opts.Stdout,
		Stderr:     opts.Stderr,
		Tty:        opts.TTY,
	}

	return r.docker.Exec(ctx, containerName, execConfig)
}

// GetContainerWorkspaceFolder returns the workspace folder in the container.
func (r *UnifiedRunner) GetContainerWorkspaceFolder() string {
	return r.workspace.Resolved.WorkspaceFolder
}

// GetPrimaryContainerName returns the primary container name.
func (r *UnifiedRunner) GetPrimaryContainerName() string {
	return r.workspace.Resolved.ServiceName
}

// GetResolvedFeatures returns the resolved features.
func (r *UnifiedRunner) GetResolvedFeatures() []*features.Feature {
	return r.resolvedFeatures
}

// Compose helper methods

func (r *UnifiedRunner) composeBaseArgs() []string {
	ws := r.workspace

	// Use compose project name for -p flag
	projectName := ws.Resolved.ServiceName
	if ws.Resolved.Compose != nil && ws.Resolved.Compose.ProjectName != "" {
		projectName = ws.Resolved.Compose.ProjectName
	}
	args := []string{"-p", projectName}

	// Add compose files
	if ws.Resolved.Compose != nil {
		for _, f := range ws.Resolved.Compose.Files {
			args = append(args, "-f", f)
		}
	}

	// Add override file if present
	if r.overridePath != "" {
		args = append(args, "-f", r.overridePath)
	}

	return args
}

func (r *UnifiedRunner) runCompose(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = r.workspace.ConfigDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func (r *UnifiedRunner) generateComposeOverride() (string, error) {
	ws := r.workspace

	var sb strings.Builder
	sb.WriteString("# Generated by dcx - do not edit\n")
	sb.WriteString("services:\n")
	sb.WriteString(fmt.Sprintf("  %s:\n", ws.Resolved.Compose.Service))

	// Add labels
	sb.WriteString("    labels:\n")
	for k, v := range r.buildLabels() {
		sb.WriteString(fmt.Sprintf("      %s: %q\n", k, v))
	}

	// Add derived image if features were installed
	if r.derivedImage != "" {
		sb.WriteString(fmt.Sprintf("    image: %s\n", r.derivedImage))
	}

	// Add forwardPorts
	if len(ws.Resolved.ForwardPorts) > 0 {
		sb.WriteString("    ports:\n")
		for _, port := range ws.Resolved.ForwardPorts {
			if port.HostPort == port.ContainerPort {
				sb.WriteString(fmt.Sprintf("      - \"%d\"\n", port.ContainerPort))
			} else {
				sb.WriteString(fmt.Sprintf("      - \"%d:%d\"\n", port.HostPort, port.ContainerPort))
			}
		}
	}

	// Add mounts (from base config and features)
	mounts := r.buildMounts()
	if len(mounts) > 0 {
		sb.WriteString("    volumes:\n")
		for _, m := range mounts {
			sb.WriteString(fmt.Sprintf("      - %q\n", m))
		}
	}

	return sb.String(), nil
}

func (r *UnifiedRunner) ensureServicesBuilt(ctx context.Context) error {
	args := r.composeBaseArgs()
	args = append(args, "build")
	return r.runCompose(ctx, args)
}

func (r *UnifiedRunner) buildDerivedImageForCompose(ctx context.Context, opts UpOptions) error {
	// Get the base image from the primary service
	baseImage, err := r.getComposeBaseImage(ctx)
	if err != nil {
		return fmt.Errorf("failed to determine base image: %w", err)
	}

	derivedImage, err := r.buildDerivedImage(ctx, baseImage, opts.Rebuild, opts.Pull)
	if err != nil {
		return err
	}

	r.derivedImage = derivedImage
	return nil
}

// applyUIDUpdateForCompose applies UID update layer for compose without features.
func (r *UnifiedRunner) applyUIDUpdateForCompose(ctx context.Context, opts UpOptions) error {
	ws := r.workspace

	// Use pre-computed decision from workspace builder
	if !ws.Build.ShouldUpdateUID {
		return nil
	}

	// Get the base image from compose
	baseImage, err := r.getComposeBaseImage(ctx)
	if err != nil {
		return fmt.Errorf("failed to determine base image: %w", err)
	}

	// Apply UID update layer
	uidImage, err := r.applyUIDUpdateLayer(ctx, baseImage, opts.Rebuild)
	if err != nil {
		return err
	}

	if uidImage != baseImage {
		r.derivedImage = uidImage
	}

	return nil
}

// getComposeBaseImage determines the base image for the primary service.
func (r *UnifiedRunner) getComposeBaseImage(ctx context.Context) (string, error) {
	ws := r.workspace

	// First check if image is directly specified in resolved config
	if ws.Resolved.Image != "" {
		return ws.Resolved.Image, nil
	}

	if ws.Resolved.Compose == nil {
		return "", fmt.Errorf("no compose configuration found")
	}

	paths := ws.Resolved.Compose.Files
	if len(paths) == 0 {
		return "", fmt.Errorf("no compose files specified")
	}

	// Get the directory of the first compose file as working directory
	workDir := filepath.Dir(paths[0])

	// Parse compose files using compose-go
	options, err := composecli.NewProjectOptions(
		paths,
		composecli.WithWorkingDirectory(workDir),
		composecli.WithOsEnv,
		composecli.WithDotEnv,
		composecli.WithInterpolation(true),
		composecli.WithResolvedPaths(true),
		composecli.WithProfiles([]string{}),
		composecli.WithDiscardEnvFile,
	)
	if err != nil {
		return "", fmt.Errorf("failed to create project options: %w", err)
	}

	project, err := options.LoadProject(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to load compose project: %w", err)
	}

	serviceName := ws.Resolved.Compose.Service
	if serviceName == "" {
		return "", fmt.Errorf("no primary service specified")
	}

	// Find the service in the project
	for _, svc := range project.Services {
		if svc.Name == serviceName {
			// If service has an image, use it
			if svc.Image != "" {
				return svc.Image, nil
			}
			// If service has a build, we can't determine base image without building
			// Return the built image name format: <project>-<service>:latest
			projectName := ws.Resolved.Compose.ProjectName
			if projectName == "" {
				projectName = ws.Resolved.ServiceName
			}
			return fmt.Sprintf("%s-%s", projectName, serviceName), nil
		}
	}

	return "", fmt.Errorf("service %q not found in compose file", serviceName)
}

func (r *UnifiedRunner) writeToTempFile(content, pattern string) (string, error) {
	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}

	tmpFile.Close()
	return tmpFile.Name(), nil
}

// MergeImageMetadata reads image metadata labels and merges with config.
func (r *UnifiedRunner) MergeImageMetadata(ctx context.Context, imageRef string) error {
	labels, err := r.docker.GetImageLabels(ctx, imageRef)
	if err != nil {
		return err
	}

	metadataLabel, ok := labels[config.DevcontainerMetadataLabel]
	if !ok || metadataLabel == "" {
		return nil // No metadata to merge
	}

	imageConfigs, err := config.ParseImageMetadata(metadataLabel)
	if err != nil {
		return fmt.Errorf("failed to parse image metadata: %w", err)
	}

	if len(imageConfigs) == 0 {
		return nil
	}

	// Merge metadata with workspace config
	if r.workspace.RawConfig != nil {
		r.workspace.RawConfig = config.MergeMetadata(r.workspace.RawConfig, imageConfigs)
	}

	return nil
}

// Verify UnifiedRunner implements Environment interface
var _ Environment = (*UnifiedRunner)(nil)
