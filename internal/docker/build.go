package docker

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/griffithind/dcx/internal/ssh/agent"
)

// CreateContainerOptions contains options for creating a container.
type CreateContainerOptions struct {
	Name            string
	Image           string
	WorkspacePath   string
	WorkspaceFolder string // Container working directory (e.g., /workspaces/project)
	WorkspaceMount  string // Mount specification (e.g., type=bind,source=...,target=...)
	Labels          map[string]string
	Env             []string
	Mounts          []string
	RunArgs         []string
	User            string
	Privileged      bool
	Init            bool
	CapAdd          []string
	CapDrop         []string
	SecurityOpt     []string
	SSHAuthSock     string
	SSHMountPath    string
	NetworkMode     string
	IpcMode         string
	PidMode         string
	ShmSize         int64
	Devices         []string
	ExtraHosts      []string
	Tmpfs           map[string]string
	Sysctls         map[string]string
	Ports           []string // Port bindings in format "hostPort:containerPort" or "containerPort"
	Entrypoint      []string // Override container entrypoint
	Cmd             []string // Override container command
}

// CreateContainer creates a new container.
func (c *Client) CreateContainer(ctx context.Context, opts CreateContainerOptions) (string, error) {
	// Build host config
	hostConfig := &container.HostConfig{
		Privileged:  opts.Privileged,
		Init:        &opts.Init,
		CapAdd:      opts.CapAdd,
		CapDrop:     opts.CapDrop,
		SecurityOpt: opts.SecurityOpt,
		ExtraHosts:  opts.ExtraHosts,
		Sysctls:     opts.Sysctls,
	}

	// Set network mode
	if opts.NetworkMode != "" {
		hostConfig.NetworkMode = container.NetworkMode(opts.NetworkMode)
	}

	// Set IPC mode
	if opts.IpcMode != "" {
		hostConfig.IpcMode = container.IpcMode(opts.IpcMode)
	}

	// Set PID mode
	if opts.PidMode != "" {
		hostConfig.PidMode = container.PidMode(opts.PidMode)
	}

	// Set shared memory size
	if opts.ShmSize > 0 {
		hostConfig.ShmSize = opts.ShmSize
	}

	// Add devices
	for _, device := range opts.Devices {
		hostConfig.Devices = append(hostConfig.Devices, container.DeviceMapping{
			PathOnHost:        device,
			PathInContainer:   device,
			CgroupPermissions: "rwm",
		})
	}

	// Add tmpfs mounts
	if len(opts.Tmpfs) > 0 {
		hostConfig.Tmpfs = opts.Tmpfs
	}

	// Add workspace bind mount
	if opts.WorkspaceMount != "" {
		// Parse Docker --mount format (type=bind,source=...,target=...)
		bind := parseMountSpec(opts.WorkspaceMount)
		if bind != "" {
			hostConfig.Binds = append(hostConfig.Binds, bind)
		}
	} else if opts.WorkspacePath != "" && opts.WorkspaceFolder != "" {
		// Default simple bind mount
		hostConfig.Binds = append(hostConfig.Binds, fmt.Sprintf("%s:%s", opts.WorkspacePath, opts.WorkspaceFolder))
	}

	// Add SSH mount if provided
	if opts.SSHMountPath != "" {
		if agent.IsDockerDesktop() {
			// On Docker Desktop, mount the host-services directory directly
			hostConfig.Binds = append(hostConfig.Binds, fmt.Sprintf("%s:%s:ro", opts.SSHMountPath, opts.SSHMountPath))
		} else {
			// On native Docker, mount the proxy directory
			hostConfig.Binds = append(hostConfig.Binds, fmt.Sprintf("%s:/ssh-agent:ro", opts.SSHMountPath))
		}
	}

	// Parse additional mounts
	for _, mount := range opts.Mounts {
		hostConfig.Binds = append(hostConfig.Binds, mount)
	}

	// Parse port bindings
	exposedPorts, portBindings := parsePortBindings(opts.Ports)
	if len(portBindings) > 0 {
		hostConfig.PortBindings = portBindings
	}

	// Build container config
	containerConfig := &container.Config{
		Image:        opts.Image,
		Labels:       opts.Labels,
		Env:          opts.Env,
		User:         opts.User,
		WorkingDir:   opts.WorkspaceFolder,
		Tty:          true,
		OpenStdin:    true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		ExposedPorts: exposedPorts,
	}

	// Override entrypoint if specified
	if len(opts.Entrypoint) > 0 {
		containerConfig.Entrypoint = opts.Entrypoint
	}

	// Override command if specified
	if len(opts.Cmd) > 0 {
		containerConfig.Cmd = opts.Cmd
	}

	// Create container
	resp, err := c.cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, opts.Name)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}

	return resp.ID, nil
}

// ImageBuildOptions contains options for building a Docker image.
type ImageBuildOptions struct {
	Tag        string
	Dockerfile string
	Context    string
	Args       map[string]string
	Target     string
	CacheFrom  []string
	ConfigDir  string    // Directory containing the devcontainer.json (for resolving relative paths)
	Stdout     io.Writer // Output stream for build output (nil = discard)
	Stderr     io.Writer // Error stream for build output (nil = discard)
}

// BuildImage builds a Docker image from a Dockerfile.
func (c *Client) BuildImage(ctx context.Context, opts ImageBuildOptions) error {
	// For single-container builds, we shell out to docker build
	// This is simpler and more compatible than using the API directly
	return buildImageWithCLI(ctx, opts)
}

// BuildImageCLI builds a Docker image using the CLI.
// This is the canonical function for all docker build operations.
// It can be called without a Client instance.
func BuildImageCLI(ctx context.Context, opts ImageBuildOptions) error {
	return buildImageWithCLI(ctx, opts)
}

// buildImageWithCLI builds an image using the docker CLI.
func buildImageWithCLI(ctx context.Context, opts ImageBuildOptions) error {
	// Determine the config directory (for resolving relative paths)
	configDir := opts.ConfigDir
	if configDir == "" {
		configDir = "."
	}

	// Resolve context path relative to config directory
	contextPath := opts.Context
	if contextPath == "" {
		contextPath = configDir
	} else if !filepath.IsAbs(contextPath) {
		contextPath = filepath.Join(configDir, contextPath)
	}

	args := []string{"build"}

	// Add tag
	if opts.Tag != "" {
		args = append(args, "-t", opts.Tag)
	}

	// Add dockerfile - resolve relative to config directory
	if opts.Dockerfile != "" {
		dockerfilePath := opts.Dockerfile
		if !filepath.IsAbs(dockerfilePath) {
			dockerfilePath = filepath.Join(configDir, dockerfilePath)
		}
		args = append(args, "-f", dockerfilePath)
	}

	// Add target
	if opts.Target != "" {
		args = append(args, "--target", opts.Target)
	}

	// Add build args
	for key, value := range opts.Args {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	// Add cache-from
	for _, cache := range opts.CacheFrom {
		args = append(args, "--cache-from", cache)
	}

	// Add SSH agent forwarding for build if available
	if agent.IsAvailable() {
		args = append(args, "--ssh", "default")
	}

	// Add context path
	args = append(args, contextPath)

	// Execute docker build
	cmd := execCommand(ctx, "docker", args...)
	if opts.Stdout != nil {
		cmd.Stdout = opts.Stdout
	} else {
		cmd.Stdout = io.Discard
	}
	if opts.Stderr != nil {
		cmd.Stderr = opts.Stderr
	} else {
		cmd.Stderr = io.Discard
	}

	return cmd.Run()
}

// execCommand is a variable to allow mocking in tests
var execCommand = execCommandReal

func execCommandReal(ctx context.Context, name string, args ...string) *execCmd {
	return &execCmd{exec.CommandContext(ctx, name, args...)}
}

type execCmd struct {
	*exec.Cmd
}
