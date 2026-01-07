// Package single provides single-container devcontainer support.
package single

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/state"
)

// Runner manages single-container devcontainer operations.
type Runner struct {
	dockerClient  *docker.Client
	workspacePath string
	configPath    string
	cfg           *config.DevcontainerConfig
	projectName   string // User-defined project name from dcx.json
	envKey        string
	configHash    string
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

// UpOptions contains options for bringing up the environment.
type UpOptions struct {
	Build bool
}

// Up creates and starts the single-container environment.
func (r *Runner) Up(ctx context.Context, opts UpOptions) error {
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
func (r *Runner) resolveImage(ctx context.Context, opts UpOptions) (string, error) {
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
	derivedImage, err := r.buildDerivedImage(ctx, baseImage)
	if err != nil {
		return "", fmt.Errorf("failed to build derived image with features: %w", err)
	}

	return derivedImage, nil
}

// buildDerivedImage builds an image with features installed on top of the base image.
func (r *Runner) buildDerivedImage(ctx context.Context, baseImage string) (string, error) {
	fmt.Println("Resolving features...")

	// Create feature manager
	configDir := filepath.Dir(r.configPath)
	mgr, err := features.NewManager(configDir)
	if err != nil {
		return "", fmt.Errorf("failed to create feature manager: %w", err)
	}

	// Resolve and order features
	resolvedFeatures, err := mgr.ResolveAll(ctx, r.cfg.Features, r.cfg.OverrideFeatureInstallOrder)
	if err != nil {
		return "", fmt.Errorf("failed to resolve features: %w", err)
	}

	fmt.Printf("Resolved %d features:\n", len(resolvedFeatures))
	for _, f := range resolvedFeatures {
		name := f.ID
		if f.Metadata != nil && f.Metadata.Name != "" {
			name = f.Metadata.Name
		}
		fmt.Printf("  - %s\n", name)
	}

	// Determine derived image tag
	derivedTag := features.GetDerivedImageTag(r.envKey, r.configHash)

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
	}

	return r.dockerClient.BuildImage(ctx, buildOpts)
}

// createContainer creates a new container with the devcontainer configuration.
func (r *Runner) createContainer(ctx context.Context, imageRef string) (string, error) {
	containerName := r.getContainerName()

	// Prepare container configuration
	createOpts := docker.CreateContainerOptions{
		Name:           containerName,
		Image:          imageRef,
		WorkspacePath:  r.workspacePath,
		WorkspaceMount: config.DetermineContainerWorkspaceFolder(r.cfg, r.workspacePath),
		Labels:         r.buildLabels(),
		Env:            r.buildEnv(),
		Mounts:         r.cfg.Mounts,
		RunArgs:        r.cfg.RunArgs,
		User:           r.cfg.RemoteUser,
		Privileged:     r.cfg.Privileged != nil && *r.cfg.Privileged,
		Init:           r.cfg.Init != nil && *r.cfg.Init,
		CapAdd:         r.cfg.CapAdd,
		SecurityOpt:    r.cfg.SecurityOpt,
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

// parseRunArgs extracts additional options from runArgs.
func (r *Runner) parseRunArgs(opts *docker.CreateContainerOptions) {
	for i := 0; i < len(r.cfg.RunArgs); i++ {
		arg := r.cfg.RunArgs[i]

		switch {
		// Cap drop
		case hasPrefix(arg, "--cap-drop="):
			opts.CapDrop = append(opts.CapDrop, trimPrefix(arg, "--cap-drop="))
		case arg == "--cap-drop" && i+1 < len(r.cfg.RunArgs):
			i++
			opts.CapDrop = append(opts.CapDrop, r.cfg.RunArgs[i])

		// Network mode
		case hasPrefix(arg, "--network="):
			opts.NetworkMode = trimPrefix(arg, "--network=")
		case hasPrefix(arg, "--net="):
			opts.NetworkMode = trimPrefix(arg, "--net=")
		case arg == "--network" && i+1 < len(r.cfg.RunArgs):
			i++
			opts.NetworkMode = r.cfg.RunArgs[i]
		case arg == "--net" && i+1 < len(r.cfg.RunArgs):
			i++
			opts.NetworkMode = r.cfg.RunArgs[i]

		// IPC mode
		case hasPrefix(arg, "--ipc="):
			opts.IpcMode = trimPrefix(arg, "--ipc=")
		case arg == "--ipc" && i+1 < len(r.cfg.RunArgs):
			i++
			opts.IpcMode = r.cfg.RunArgs[i]

		// PID mode
		case hasPrefix(arg, "--pid="):
			opts.PidMode = trimPrefix(arg, "--pid=")
		case arg == "--pid" && i+1 < len(r.cfg.RunArgs):
			i++
			opts.PidMode = r.cfg.RunArgs[i]

		// Shared memory size
		case hasPrefix(arg, "--shm-size="):
			opts.ShmSize = parseShmSize(trimPrefix(arg, "--shm-size="))
		case arg == "--shm-size" && i+1 < len(r.cfg.RunArgs):
			i++
			opts.ShmSize = parseShmSize(r.cfg.RunArgs[i])

		// Devices
		case hasPrefix(arg, "--device="):
			opts.Devices = append(opts.Devices, trimPrefix(arg, "--device="))
		case arg == "--device" && i+1 < len(r.cfg.RunArgs):
			i++
			opts.Devices = append(opts.Devices, r.cfg.RunArgs[i])

		// Extra hosts
		case hasPrefix(arg, "--add-host="):
			opts.ExtraHosts = append(opts.ExtraHosts, trimPrefix(arg, "--add-host="))
		case arg == "--add-host" && i+1 < len(r.cfg.RunArgs):
			i++
			opts.ExtraHosts = append(opts.ExtraHosts, r.cfg.RunArgs[i])

		// Tmpfs
		case hasPrefix(arg, "--tmpfs="):
			if opts.Tmpfs == nil {
				opts.Tmpfs = make(map[string]string)
			}
			parseTmpfs(opts.Tmpfs, trimPrefix(arg, "--tmpfs="))
		case arg == "--tmpfs" && i+1 < len(r.cfg.RunArgs):
			i++
			if opts.Tmpfs == nil {
				opts.Tmpfs = make(map[string]string)
			}
			parseTmpfs(opts.Tmpfs, r.cfg.RunArgs[i])

		// Sysctl
		case hasPrefix(arg, "--sysctl="):
			if opts.Sysctls == nil {
				opts.Sysctls = make(map[string]string)
			}
			parseSysctl(opts.Sysctls, trimPrefix(arg, "--sysctl="))
		case arg == "--sysctl" && i+1 < len(r.cfg.RunArgs):
			i++
			if opts.Sysctls == nil {
				opts.Sysctls = make(map[string]string)
			}
			parseSysctl(opts.Sysctls, r.cfg.RunArgs[i])
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

// Down removes the container.
func (r *Runner) Down(ctx context.Context, removeVolumes bool) error {
	return r.dockerClient.RemoveContainer(ctx, r.getContainerName(), true)
}

// GetContainerWorkspaceFolder returns the workspace folder path in the container.
func (r *Runner) GetContainerWorkspaceFolder() string {
	return config.DetermineContainerWorkspaceFolder(r.cfg, r.workspacePath)
}

// Helper functions for parsing runArgs

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func trimPrefix(s, prefix string) string {
	if hasPrefix(s, prefix) {
		return s[len(prefix):]
	}
	return s
}

// parseShmSize parses a shared memory size string (e.g., "1g", "512m", "1024").
func parseShmSize(size string) int64 {
	if size == "" {
		return 0
	}

	// Remove any whitespace
	size = trimSpace(size)

	var multiplier int64 = 1
	lastChar := size[len(size)-1]

	switch lastChar {
	case 'k', 'K':
		multiplier = 1024
		size = size[:len(size)-1]
	case 'm', 'M':
		multiplier = 1024 * 1024
		size = size[:len(size)-1]
	case 'g', 'G':
		multiplier = 1024 * 1024 * 1024
		size = size[:len(size)-1]
	case 'b', 'B':
		size = size[:len(size)-1]
	}

	var num int64
	for _, c := range size {
		if c >= '0' && c <= '9' {
			num = num*10 + int64(c-'0')
		}
	}

	return num * multiplier
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}

// parseTmpfs parses a tmpfs mount specification (e.g., "/run:size=100m").
func parseTmpfs(tmpfs map[string]string, value string) {
	// Format: /path or /path:options
	parts := splitN(value, ":", 2)
	if len(parts) == 1 {
		tmpfs[parts[0]] = ""
	} else {
		tmpfs[parts[0]] = parts[1]
	}
}

// parseSysctl parses a sysctl key=value pair.
func parseSysctl(sysctls map[string]string, value string) {
	parts := splitN(value, "=", 2)
	if len(parts) == 2 {
		sysctls[parts[0]] = parts[1]
	}
}

func splitN(s, sep string, n int) []string {
	if n <= 0 {
		return nil
	}
	var result []string
	for i := 0; i < n-1; i++ {
		idx := indexOf(s, sep)
		if idx < 0 {
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
