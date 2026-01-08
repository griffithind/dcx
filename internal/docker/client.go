// Package docker provides a wrapper around the Docker Engine API client.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/griffithind/dcx/internal/ssh"
)

// Client wraps the Docker client with dcx-specific functionality.
type Client struct {
	cli *client.Client
}

// Container represents a Docker container.
type Container struct {
	ID         string
	Name       string
	Image      string
	Status     string
	State      string
	Labels     map[string]string
	Created    time.Time
	Running    bool
}

// NewClient creates a new Docker client.
func NewClient() (*Client, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &Client{cli: cli}, nil
}

// Close closes the Docker client.
func (c *Client) Close() error {
	return c.cli.Close()
}

// Ping checks if the Docker daemon is accessible.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// ServerVersion returns the Docker server version.
func (c *Client) ServerVersion(ctx context.Context) (string, error) {
	version, err := c.cli.ServerVersion(ctx)
	if err != nil {
		return "", err
	}
	return version.Version, nil
}

// SystemInfo contains information about the Docker daemon's resources.
type SystemInfo struct {
	NCPU        int    // Number of CPUs available to Docker
	MemTotal    uint64 // Total memory available to Docker in bytes
	OSType      string // Operating system type (linux, windows)
	Architecture string // Architecture (x86_64, arm64, etc.)
}

// Info returns system-wide information about Docker.
// This reflects Docker's configured resource limits, which may be less than the host's
// actual resources (e.g., Docker Desktop VM limits, cgroup limits).
func (c *Client) Info(ctx context.Context) (*SystemInfo, error) {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Docker info: %w", err)
	}

	return &SystemInfo{
		NCPU:        info.NCPU,
		MemTotal:    uint64(info.MemTotal),
		OSType:      info.OSType,
		Architecture: info.Architecture,
	}, nil
}

// ListContainers returns containers matching the given label filters.
func (c *Client) ListContainers(ctx context.Context, labelFilters map[string]string) ([]Container, error) {
	filterArgs := filters.NewArgs()
	for key, value := range labelFilters {
		filterArgs.Add("label", fmt.Sprintf("%s=%s", key, value))
	}

	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]Container, 0, len(containers))
	for _, ctr := range containers {
		name := ""
		if len(ctr.Names) > 0 {
			name = ctr.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
		}

		result = append(result, Container{
			ID:      ctr.ID,
			Name:    name,
			Image:   ctr.Image,
			Status:  ctr.Status,
			State:   ctr.State,
			Labels:  ctr.Labels,
			Created: time.Unix(ctr.Created, 0),
			Running: ctr.State == "running",
		})
	}

	return result, nil
}

// InspectContainer returns detailed information about a container.
func (c *Client) InspectContainer(ctx context.Context, containerID string) (*Container, error) {
	info, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	name := info.Name
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	created, _ := time.Parse(time.RFC3339Nano, info.Created)

	return &Container{
		ID:      info.ID,
		Name:    name,
		Image:   info.Config.Image,
		Status:  info.State.Status,
		State:   info.State.Status,
		Labels:  info.Config.Labels,
		Created: created,
		Running: info.State.Running,
	}, nil
}

// StartContainer starts a stopped container.
func (c *Client) StartContainer(ctx context.Context, containerID string) error {
	return c.cli.ContainerStart(ctx, containerID, container.StartOptions{})
}

// StopContainer stops a running container.
func (c *Client) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	var timeoutSecs *int
	if timeout != nil {
		secs := int(timeout.Seconds())
		timeoutSecs = &secs
	}
	return c.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: timeoutSecs})
}

// RemoveContainer removes a container.
// If removeVolumes is true, anonymous volumes attached to the container are also removed.
func (c *Client) RemoveContainer(ctx context.Context, containerID string, force, removeVolumes bool) error {
	return c.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         force,
		RemoveVolumes: removeVolumes,
	})
}

// ExecConfig contains configuration for exec operations.
type ExecConfig struct {
	Cmd        []string
	Env        []string
	WorkingDir string
	User       string
	Tty        bool
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
}

// Exec runs a command inside a container.
func (c *Client) Exec(ctx context.Context, containerID string, config ExecConfig) (int, error) {
	execConfig := container.ExecOptions{
		Cmd:          config.Cmd,
		Env:          config.Env,
		WorkingDir:   config.WorkingDir,
		User:         config.User,
		Tty:          config.Tty,
		AttachStdin:  config.Stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
	}

	resp, err := c.cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return -1, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := c.cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{
		Tty: config.Tty,
	})
	if err != nil {
		return -1, fmt.Errorf("failed to attach exec: %w", err)
	}
	defer attachResp.Close()

	// Handle I/O in goroutines for interactive mode
	errCh := make(chan error, 2)

	// Copy stdin if provided
	if config.Stdin != nil {
		go func() {
			_, err := io.Copy(attachResp.Conn, config.Stdin)
			// Close write side to signal EOF
			if cw, ok := attachResp.Conn.(interface{ CloseWrite() error }); ok {
				cw.CloseWrite()
			}
			errCh <- err
		}()
	}

	// Copy output
	go func() {
		if config.Tty {
			// In TTY mode, stdout and stderr are combined
			if config.Stdout != nil {
				_, err := io.Copy(config.Stdout, attachResp.Reader)
				errCh <- err
				return
			}
		} else {
			// Non-TTY mode, demux stdout and stderr
			_, err := StdCopy(config.Stdout, config.Stderr, attachResp.Reader)
			errCh <- err
			return
		}
		errCh <- nil
	}()

	// Wait for output to complete
	<-errCh
	if config.Stdin != nil {
		<-errCh
	}

	// Get exit code
	inspectResp, err := c.cli.ContainerExecInspect(ctx, resp.ID)
	if err != nil {
		return -1, fmt.Errorf("failed to inspect exec: %w", err)
	}

	return inspectResp.ExitCode, nil
}

// StdCopy demultiplexes Docker's multiplexed streams.
// Docker multiplexes stdout and stderr into a single stream when not using a TTY.
// This function properly separates them using the Docker protocol.
func StdCopy(stdout, stderr io.Writer, src io.Reader) (written int64, err error) {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return stdcopy.StdCopy(stdout, stderr, src)
}

// ImageExists checks if an image exists locally.
func (c *Client) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	_, _, err := c.cli.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetImageLabels returns the labels for an image.
func (c *Client) GetImageLabels(ctx context.Context, imageRef string) (map[string]string, error) {
	info, _, err := c.cli.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}
	if info.Config == nil {
		return nil, nil
	}
	return info.Config.Labels, nil
}

// PullImage pulls an image from a registry.
func (c *Client) PullImage(ctx context.Context, imageRef string) error {
	return c.PullImageWithProgress(ctx, imageRef, nil)
}

// PullImageWithProgress pulls an image with optional progress display.
// If progressOut is nil, progress is discarded.
func (c *Client) PullImageWithProgress(ctx context.Context, imageRef string, progressOut io.Writer) error {
	reader, err := c.cli.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer reader.Close()

	if progressOut == nil {
		// No progress display - just consume output
		_, err = io.Copy(io.Discard, reader)
		return err
	}

	// Process with progress display
	display := newProgressDisplay(progressOut)
	return display.processPullOutput(reader)
}

// progressDisplay handles Docker progress output.
type progressDisplay struct {
	out io.Writer
}

func newProgressDisplay(out io.Writer) *progressDisplay {
	return &progressDisplay{out: out}
}

// progressEvent represents a Docker progress event.
type progressEvent struct {
	ID             string                 `json:"id,omitempty"`
	Status         string                 `json:"status,omitempty"`
	Progress       string                 `json:"progress,omitempty"`
	ProgressDetail progressDetail         `json:"progressDetail,omitempty"`
	Stream         string                 `json:"stream,omitempty"`
	Error          string                 `json:"error,omitempty"`
}

type progressDetail struct {
	Current int64 `json:"current,omitempty"`
	Total   int64 `json:"total,omitempty"`
}

func (d *progressDisplay) processPullOutput(reader io.Reader) error {
	decoder := json.NewDecoder(reader)
	layers := make(map[string]string)
	var lastStatus string

	for {
		var event progressEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		if event.Error != "" {
			return fmt.Errorf("%s", event.Error)
		}

		// Track layer status
		if event.ID != "" {
			layers[event.ID] = event.Status
		}

		// Show meaningful status updates
		status := event.Status
		if event.ID != "" && event.Progress != "" {
			id := event.ID
			if len(id) > 12 {
				id = id[:12]
			}
			status = fmt.Sprintf("%s: %s %s", id, event.Status, event.Progress)
		} else if event.ID != "" {
			id := event.ID
			if len(id) > 12 {
				id = id[:12]
			}
			status = fmt.Sprintf("%s: %s", id, event.Status)
		}

		if status != "" && status != lastStatus {
			fmt.Fprintf(d.out, "%s\n", status)
			lastStatus = status
		}
	}

	return nil
}

// CreateContainerOptions contains options for creating a container.
type CreateContainerOptions struct {
	Name            string
	Image           string
	WorkspacePath   string
	WorkspaceFolder string // Container working directory (e.g., /workspaces/project)
	WorkspaceMount  string // Mount specification (e.g., type=bind,source=...,target=...)
	Labels          map[string]string
	Env            []string
	Mounts         []string
	RunArgs        []string
	User           string
	Privileged     bool
	Init           bool
	CapAdd         []string
	CapDrop        []string
	SecurityOpt    []string
	SSHAuthSock    string
	SSHMountPath   string
	NetworkMode    string
	IpcMode        string
	PidMode        string
	ShmSize        int64
	Devices        []string
	ExtraHosts     []string
	Tmpfs          map[string]string
	Sysctls        map[string]string
	Ports          []string // Port bindings in format "hostPort:containerPort" or "containerPort"
	Entrypoint     []string // Override container entrypoint
	Cmd            []string // Override container command
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
	if opts.WorkspacePath != "" && opts.WorkspaceMount != "" {
		hostConfig.Binds = append(hostConfig.Binds, fmt.Sprintf("%s:%s", opts.WorkspacePath, opts.WorkspaceMount))
	}

	// Add SSH mount if provided
	if opts.SSHMountPath != "" {
		if ssh.IsDockerDesktop() {
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

// parsePortBindings parses port specifications into exposed ports and port bindings.
// Supports formats: "8080", "8080:80", "127.0.0.1:8080:80", "8080/udp"
func parsePortBindings(ports []string) (nat.PortSet, nat.PortMap) {
	exposedPorts := make(nat.PortSet)
	portBindings := make(nat.PortMap)

	for _, portSpec := range ports {
		hostIP := ""
		hostPort := ""
		containerPort := ""
		protocol := "tcp"

		// Check for protocol suffix
		if idx := strings.LastIndex(portSpec, "/"); idx != -1 {
			protocol = portSpec[idx+1:]
			portSpec = portSpec[:idx]
		}

		parts := strings.Split(portSpec, ":")
		switch len(parts) {
		case 1:
			// Just container port, bind to same host port
			containerPort = parts[0]
			hostPort = parts[0]
		case 2:
			// hostPort:containerPort
			hostPort = parts[0]
			containerPort = parts[1]
		case 3:
			// hostIP:hostPort:containerPort
			hostIP = parts[0]
			hostPort = parts[1]
			containerPort = parts[2]
		default:
			continue
		}

		// Validate port numbers
		if _, err := strconv.Atoi(containerPort); err != nil {
			continue
		}

		natPort := nat.Port(fmt.Sprintf("%s/%s", containerPort, protocol))
		exposedPorts[natPort] = struct{}{}
		portBindings[natPort] = []nat.PortBinding{
			{
				HostIP:   hostIP,
				HostPort: hostPort,
			},
		}
	}

	return exposedPorts, portBindings
}

// BuildOptions contains options for building an image.
type BuildOptions struct {
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
func (c *Client) BuildImage(ctx context.Context, opts BuildOptions) error {
	// For single-container builds, we shell out to docker build
	// This is simpler and more compatible than using the API directly
	return buildImageWithCLI(ctx, opts)
}

// buildImageWithCLI builds an image using the docker CLI.
func buildImageWithCLI(ctx context.Context, opts BuildOptions) error {
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
	if ssh.IsAgentAvailable() {
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

// LogsOptions contains options for retrieving container logs.
type LogsOptions struct {
	Follow     bool
	Timestamps bool
	Tail       string // Number of lines or "all"
}

// GetLogs retrieves logs from a container.
func (c *Client) GetLogs(ctx context.Context, containerID string, opts LogsOptions) (io.ReadCloser, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     opts.Follow,
		Timestamps: opts.Timestamps,
	}

	if opts.Tail != "" && opts.Tail != "all" {
		options.Tail = opts.Tail
	}

	return c.cli.ContainerLogs(ctx, containerID, options)
}

// AttachOptions contains options for attaching to a container.
type AttachOptions struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	TTY        bool
	DetachKeys string
}

// AttachContainer attaches to a running container's streams.
func (c *Client) AttachContainer(ctx context.Context, containerID string, opts AttachOptions) error {
	attachOpts := container.AttachOptions{
		Stream: true,
		Stdin:  opts.Stdin != nil,
		Stdout: true,
		Stderr: true,
	}

	if opts.DetachKeys != "" {
		attachOpts.DetachKeys = opts.DetachKeys
	}

	resp, err := c.cli.ContainerAttach(ctx, containerID, attachOpts)
	if err != nil {
		return fmt.Errorf("failed to attach: %w", err)
	}
	defer resp.Close()

	// Handle I/O
	errCh := make(chan error, 2)

	// Copy stdin if provided
	if opts.Stdin != nil {
		go func() {
			_, err := io.Copy(resp.Conn, opts.Stdin)
			if cw, ok := resp.Conn.(interface{ CloseWrite() error }); ok {
				cw.CloseWrite()
			}
			errCh <- err
		}()
	}

	// Copy output
	go func() {
		if opts.TTY {
			// TTY mode - stdout and stderr are combined
			if opts.Stdout != nil {
				_, err := io.Copy(opts.Stdout, resp.Reader)
				errCh <- err
				return
			}
		} else {
			// Non-TTY mode - demux stdout/stderr
			_, err := StdCopy(opts.Stdout, opts.Stderr, resp.Reader)
			errCh <- err
			return
		}
		errCh <- nil
	}()

	// Wait for output to complete
	<-errCh
	if opts.Stdin != nil {
		<-errCh
	}

	return nil
}

// KillContainer sends a signal to a container.
func (c *Client) KillContainer(ctx context.Context, containerID, signal string) error {
	return c.cli.ContainerKill(ctx, containerID, signal)
}
