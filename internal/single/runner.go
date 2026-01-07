// Package single provides single-container devcontainer support.
package single

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/parse"
	"github.com/griffithind/dcx/internal/runner"
	"github.com/griffithind/dcx/internal/state"
)

// Runner manages single-container devcontainer operations.
type Runner struct {
	dockerClient     *docker.Client
	workspacePath    string
	configPath       string
	cfg              *config.DevcontainerConfig
	projectName      string // User-defined project name from dcx.json
	envKey           string
	configHash       string
	resolvedFeatures []*features.Feature // Resolved features (stored for runtime config)
}

// NewRunner creates a new single-container runner.
// projectName is optional - if provided, it's used directly as the container name.
// If empty, falls back to "dcx_" + envKey.
func NewRunner(dockerClient *docker.Client, workspacePath, configPath string, cfg *config.DevcontainerConfig, projectName, envKey, configHash string) *Runner {
	return &Runner{
		dockerClient:  dockerClient,
		workspacePath: workspacePath,
		configPath:    configPath,
		cfg:           cfg,
		projectName:   projectName,
		envKey:        envKey,
		configHash:    configHash,
	}
}

// getContainerName returns the container name based on project name or env key.
func (r *Runner) getContainerName() string {
	if r.projectName != "" {
		return r.projectName
	}
	return fmt.Sprintf("dcx_%s", r.envKey)
}

// Up creates and starts the single-container environment.
func (r *Runner) Up(ctx context.Context, opts runner.UpOptions) error {
	// Determine the image to use
	imageRef, err := r.resolveImage(ctx, opts)
	if err != nil {
		return err
	}

	// Create the container
	containerID, err := r.createContainer(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	// Start the container
	if err := r.dockerClient.StartContainer(ctx, containerID); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	return nil
}

// resolveImage determines which image to use, pulling or building as needed.
func (r *Runner) resolveImage(ctx context.Context, opts runner.UpOptions) (string, error) {
	var baseImage string

	// Image-based configuration
	if r.cfg.Image != "" {
		fmt.Printf("Using image: %s\n", r.cfg.Image)

		// Check if image exists locally
		exists, err := r.dockerClient.ImageExists(ctx, r.cfg.Image)
		if err != nil {
			return "", fmt.Errorf("failed to check image: %w", err)
		}

		if !exists || opts.Build {
			fmt.Printf("Pulling image: %s\n", r.cfg.Image)
			if err := r.dockerClient.PullImage(ctx, r.cfg.Image); err != nil {
				return "", fmt.Errorf("failed to pull image: %w", err)
			}
		}

		baseImage = r.cfg.Image
	} else if r.cfg.Build != nil {
		// Dockerfile-based configuration
		imageTag := fmt.Sprintf("dcx/%s:%s", r.envKey, r.configHash[:12])
		fmt.Printf("Building image: %s\n", imageTag)

		if err := r.buildImage(ctx, imageTag); err != nil {
			return "", fmt.Errorf("failed to build image: %w", err)
		}

		baseImage = imageTag
	} else {
		return "", fmt.Errorf("no image or build configuration found")
	}

	// Check if features are configured
	if len(r.cfg.Features) == 0 {
		return baseImage, nil
	}

	// Build derived image with features
	derivedImage, err := r.buildDerivedImage(ctx, baseImage, opts.Build, opts.Pull)
	if err != nil {
		return "", fmt.Errorf("failed to build derived image with features: %w", err)
	}

	return derivedImage, nil
}

// buildDerivedImage builds an image with features installed on top of the base image.
func (r *Runner) buildDerivedImage(ctx context.Context, baseImage string, forceRebuild, forcePull bool) (string, error) {
	// Determine derived image tag
	derivedTag := features.GetDerivedImageTag(r.envKey, r.configHash)

	// Check if derived image already exists (skip rebuild unless forced or pull requested)
	// When --pull is used, we need to rebuild to incorporate potentially updated features
	if !forceRebuild && !forcePull && r.imageExists(ctx, derivedTag) {
		fmt.Printf("Using cached derived image: %s\n", derivedTag)

		// Still need to resolve features for runtime config (mounts, caps, etc.)
		configDir := filepath.Dir(r.configPath)
		mgr, err := features.NewManager(configDir)
		if err != nil {
			return "", fmt.Errorf("failed to create feature manager: %w", err)
		}

		resolvedFeatures, err := mgr.ResolveAll(ctx, r.cfg.Features, r.cfg.OverrideFeatureInstallOrder)
		if err != nil {
			return "", fmt.Errorf("failed to resolve features: %w", err)
		}

		r.resolvedFeatures = resolvedFeatures
		return derivedTag, nil
	}

	if forcePull {
		fmt.Println("Re-fetching features from registry...")
	}
	fmt.Println("Resolving features...")

	// Create feature manager
	configDir := filepath.Dir(r.configPath)
	mgr, err := features.NewManager(configDir)
	if err != nil {
		return "", fmt.Errorf("failed to create feature manager: %w", err)
	}

	// Set force-pull on the manager if --pull was specified
	if forcePull {
		mgr.SetForcePull(true)
	}

	// Resolve and order features
	resolvedFeatures, err := mgr.ResolveAll(ctx, r.cfg.Features, r.cfg.OverrideFeatureInstallOrder)
	if err != nil {
		return "", fmt.Errorf("failed to resolve features: %w", err)
	}

	// Store resolved features for runtime configuration (mounts, caps, etc.)
	r.resolvedFeatures = resolvedFeatures

	fmt.Printf("Resolved %d features:\n", len(resolvedFeatures))
	for _, f := range resolvedFeatures {
		name := f.ID
		if f.Metadata != nil && f.Metadata.Name != "" {
			name = f.Metadata.Name
		}
		fmt.Printf("  - %s\n", name)
	}

	// Create build directory
	buildDir := filepath.Join(os.TempDir(), "dcx-features", r.envKey)
	defer os.RemoveAll(buildDir)

	fmt.Printf("Building derived image: %s\n", derivedTag)

	// Build the derived image
	if err := mgr.BuildDerivedImage(ctx, baseImage, derivedTag, resolvedFeatures, buildDir, r.cfg.RemoteUser); err != nil {
		return "", err
	}

	return derivedTag, nil
}

// imageExists checks if a Docker image exists locally.
func (r *Runner) imageExists(ctx context.Context, imageTag string) bool {
	exists, err := r.dockerClient.ImageExists(ctx, imageTag)
	return err == nil && exists
}

// buildImage builds an image from a Dockerfile.
func (r *Runner) buildImage(ctx context.Context, imageTag string) error {
	if r.cfg.Build == nil {
		return fmt.Errorf("no build configuration")
	}

	// ConfigDir should be the directory containing devcontainer.json
	configDir := filepath.Dir(r.configPath)

	buildOpts := docker.BuildOptions{
		Tag:        imageTag,
		Dockerfile: r.cfg.Build.Dockerfile,
		Context:    r.cfg.Build.Context,
		Args:       r.cfg.Build.Args,
		Target:     r.cfg.Build.Target,
		CacheFrom:  r.cfg.Build.CacheFrom,
		ConfigDir:  configDir,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
	}

	return r.dockerClient.BuildImage(ctx, buildOpts)
}

// createContainer creates a new container with the devcontainer configuration.
func (r *Runner) createContainer(ctx context.Context, imageRef string) (string, error) {
	containerName := r.getContainerName()

	// Collect mounts from config and features
	mounts := append([]string{}, r.cfg.Mounts...)
	if len(r.resolvedFeatures) > 0 {
		featureMounts := features.CollectMounts(r.resolvedFeatures)
		for _, mount := range featureMounts {
			parsed := parseMountString(mount)
			if parsed != "" {
				mounts = append(mounts, parsed)
			}
		}
	}

	// Collect capabilities from config and features
	capAdd := append([]string{}, r.cfg.CapAdd...)
	if len(r.resolvedFeatures) > 0 {
		featureCaps := features.CollectCapabilities(r.resolvedFeatures)
		capAdd = append(capAdd, featureCaps...)
	}

	// Collect security options from config and features
	securityOpt := append([]string{}, r.cfg.SecurityOpt...)
	if len(r.resolvedFeatures) > 0 {
		featureSecOpts := features.CollectSecurityOpts(r.resolvedFeatures)
		securityOpt = append(securityOpt, featureSecOpts...)
	}

	// Check if privileged mode is needed
	privileged := r.cfg.Privileged != nil && *r.cfg.Privileged
	if !privileged && len(r.resolvedFeatures) > 0 {
		if features.NeedsPrivileged(r.resolvedFeatures) {
			privileged = true
			// Warn user about security implications
			privFeatures := features.GetPrivilegedFeatures(r.resolvedFeatures)
			fmt.Printf("Warning: Enabling privileged mode (requested by features: %s)\n", strings.Join(privFeatures, ", "))
			fmt.Println("  Privileged mode grants full access to host devices and bypasses security features.")
		}
	}

	// Check if init is needed
	init := r.cfg.Init != nil && *r.cfg.Init
	if !init && len(r.resolvedFeatures) > 0 {
		init = features.NeedsInit(r.resolvedFeatures)
	}

	// Prepare container configuration
	createOpts := docker.CreateContainerOptions{
		Name:           containerName,
		Image:          imageRef,
		WorkspacePath:  r.workspacePath,
		WorkspaceMount: config.DetermineContainerWorkspaceFolder(r.cfg, r.workspacePath),
		Labels:         r.buildLabels(),
		Env:            r.buildEnv(),
		Mounts:         mounts,
		RunArgs:        r.cfg.RunArgs,
		User:           r.cfg.RemoteUser,
		Privileged:     privileged,
		Init:           init,
		CapAdd:         capAdd,
		SecurityOpt:    securityOpt,
	}

	// Apply overrideCommand if specified (keep container alive instead of running default command)
	if r.cfg.OverrideCommand != nil && *r.cfg.OverrideCommand {
		createOpts.Entrypoint = []string{"/bin/sh", "-c"}
		createOpts.Cmd = []string{"while sleep 1000; do :; done"}
	}

	// Parse runArgs for additional options
	r.parseRunArgs(&createOpts)

	// Add forward ports from config
	forwardPorts := r.cfg.GetForwardPorts()
	if len(forwardPorts) > 0 {
		createOpts.Ports = append(createOpts.Ports, forwardPorts...)
	}

	return r.dockerClient.CreateContainer(ctx, createOpts)
}

// parseRunArgs extracts additional options from runArgs using the shared parser.
func (r *Runner) parseRunArgs(opts *docker.CreateContainerOptions) {
	parsed := parse.ParseRunArgs(r.cfg.RunArgs)
	if parsed == nil {
		return
	}

	// Apply parsed values to options
	opts.CapDrop = append(opts.CapDrop, parsed.CapDrop...)
	opts.NetworkMode = parsed.NetworkMode
	opts.IpcMode = parsed.IpcMode
	opts.PidMode = parsed.PidMode
	opts.Devices = append(opts.Devices, parsed.Devices...)
	opts.ExtraHosts = append(opts.ExtraHosts, parsed.ExtraHosts...)

	// Convert shm-size string to int64
	if parsed.ShmSize != "" {
		opts.ShmSize = parse.ParseShmSize(parsed.ShmSize)
	}

	// Convert tmpfs list to map
	if len(parsed.Tmpfs) > 0 {
		if opts.Tmpfs == nil {
			opts.Tmpfs = make(map[string]string)
		}
		for _, spec := range parsed.Tmpfs {
			parse.ParseTmpfs(opts.Tmpfs, spec)
		}
	}

	// Copy sysctls
	if len(parsed.Sysctls) > 0 {
		if opts.Sysctls == nil {
			opts.Sysctls = make(map[string]string)
		}
		for k, v := range parsed.Sysctls {
			opts.Sysctls[k] = v
		}
	}
}

// buildLabels creates the labels for the container.
func (r *Runner) buildLabels() map[string]string {
	labels := map[string]string{
		docker.LabelManaged:    "true",
		docker.LabelEnvKey:     r.envKey,
		docker.LabelConfigHash: r.configHash,
		docker.LabelPlan:       docker.PlanSingle,
		docker.LabelPrimary:    "true",
		docker.LabelVersion:    docker.LabelSchemaVersion,
	}

	// Add workspace root hash and path
	labels[docker.LabelWorkspaceRootHash] = state.ComputeWorkspaceHash(r.workspacePath)
	labels[docker.LabelWorkspacePath] = r.workspacePath

	return labels
}

// buildEnv creates the environment variables for the container.
func (r *Runner) buildEnv() []string {
	var env []string

	// Add container env from config
	for key, value := range r.cfg.ContainerEnv {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Add remote env from config
	for key, value := range r.cfg.RemoteEnv {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}

	// Note: SSH_AUTH_SOCK is NOT set in container env because:
	// 1. The socket is created per-exec with a unique name
	// 2. Docker exec Env doesn't override existing container env
	// 3. The unique socket path is passed via exec's Env

	return env
}

// Start starts an existing stopped container.
func (r *Runner) Start(ctx context.Context) error {
	return r.dockerClient.StartContainer(ctx, r.getContainerName())
}

// Stop stops a running container.
func (r *Runner) Stop(ctx context.Context) error {
	return r.dockerClient.StopContainer(ctx, r.getContainerName(), nil)
}

// Down removes the container and optionally its volumes.
func (r *Runner) Down(ctx context.Context, opts runner.DownOptions) error {
	return r.dockerClient.RemoveContainer(ctx, r.getContainerName(), true, opts.RemoveVolumes)
}

// GetContainerWorkspaceFolder returns the workspace folder path in the container.
func (r *Runner) GetContainerWorkspaceFolder() string {
	return config.DetermineContainerWorkspaceFolder(r.cfg, r.workspacePath)
}

// GetPrimaryContainerName returns the name of the primary container.
func (r *Runner) GetPrimaryContainerName() string {
	return r.getContainerName()
}

// Build builds the container image without starting the container.
func (r *Runner) Build(ctx context.Context, opts runner.BuildOptions) error {
	// Resolve the image (may involve building from Dockerfile or features)
	_, err := r.resolveImage(ctx, runner.UpOptions{
		Build: true,
		Pull:  opts.Pull,
	})
	return err
}

// Exec executes a command in the running container.
func (r *Runner) Exec(ctx context.Context, cmd []string, opts runner.ExecOptions) (int, error) {
	containerName := r.getContainerName()

	execConfig := docker.ExecConfig{
		Cmd:        cmd,
		WorkingDir: opts.WorkingDir,
		User:       opts.User,
		Env:        opts.Env,
		Tty:        opts.TTY,
	}

	return r.dockerClient.Exec(ctx, containerName, execConfig)
}

// parseMountString parses a devcontainer mount string and returns a Docker-compatible format.
// Uses the shared parse.ParseMount for consistent parsing.
func parseMountString(mount string) string {
	m := parse.ParseMount(mount)
	if m == nil {
		return ""
	}
	return m.ToDockerFormat()
}
