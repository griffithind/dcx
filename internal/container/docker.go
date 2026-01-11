// Package container provides container runtime management for devcontainers.
// This package replaces the previous internal/docker and internal/runner packages
// with clearer naming (DockerClient, ContainerRuntime).
package container

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

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/parse"
	"github.com/griffithind/dcx/internal/state"
)

// DockerClient wraps the Docker client with dcx-specific functionality.
// This replaces the previous docker.Client type.
type DockerClient struct {
	cli *client.Client
}

// NewDockerClient creates a new Docker client.
func NewDockerClient() (*DockerClient, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	return &DockerClient{cli: cli}, nil
}

// Close closes the Docker client.
func (c *DockerClient) Close() error {
	return c.cli.Close()
}

// APIClient returns the underlying Docker API client.
func (c *DockerClient) APIClient() *client.Client {
	return c.cli
}

// Ping checks if the Docker daemon is accessible.
func (c *DockerClient) Ping(ctx context.Context) error {
	_, err := c.cli.Ping(ctx)
	return err
}

// ServerVersion returns the Docker server version.
func (c *DockerClient) ServerVersion(ctx context.Context) (string, error) {
	version, err := c.cli.ServerVersion(ctx)
	if err != nil {
		return "", err
	}
	return version.Version, nil
}

// SystemInfo contains information about the Docker daemon's resources.
type SystemInfo struct {
	NCPU         int    // Number of CPUs available to Docker
	MemTotal     uint64 // Total memory available to Docker in bytes
	OSType       string // Operating system type (linux, windows)
	Architecture string // Architecture (x86_64, arm64, etc.)
}

// Info returns system-wide information about Docker.
// This reflects Docker's configured resource limits, which may be less than the host's
// actual resources (e.g., Docker Desktop VM limits, cgroup limits).
func (c *DockerClient) Info(ctx context.Context) (*SystemInfo, error) {
	info, err := c.cli.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get Docker info: %w", err)
	}

	return &SystemInfo{
		NCPU:         info.NCPU,
		MemTotal:     uint64(info.MemTotal),
		OSType:       info.OSType,
		Architecture: info.Architecture,
	}, nil
}

// ListContainersWithLabels returns containers matching label filters.
// Implements state.ContainerClient.
func (c *DockerClient) ListContainersWithLabels(ctx context.Context, labels map[string]string) ([]state.ContainerSummary, error) {
	f := filters.NewArgs()
	for k, v := range labels {
		f.Add("label", k+"="+v)
	}

	containers, err := c.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: f,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]state.ContainerSummary, len(containers))
	for i, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		result[i] = state.ContainerSummary{
			ID:      c.ID,
			Name:    name,
			State:   c.State,
			Running: c.State == "running",
			Labels:  c.Labels,
		}
	}
	return result, nil
}

// InspectContainer returns detailed information about a container.
// Implements state.ContainerClient.
func (c *DockerClient) InspectContainer(ctx context.Context, containerID string) (*state.ContainerDetails, error) {
	info, err := c.cli.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	mounts := make([]string, len(info.Mounts))
	for i, m := range info.Mounts {
		mounts[i] = fmt.Sprintf("%s:%s", m.Source, m.Destination)
	}

	return &state.ContainerDetails{
		ID:         info.ID,
		Name:       strings.TrimPrefix(info.Name, "/"),
		State:      info.State.Status,
		Running:    info.State.Running,
		StartedAt:  info.State.StartedAt,
		Image:      info.Image,
		Labels:     info.Config.Labels,
		Mounts:     mounts,
		WorkingDir: info.Config.WorkingDir,
	}, nil
}

// Ensure DockerClient implements state.ContainerClient.
var _ state.ContainerClient = (*DockerClient)(nil)

// SanitizeProjectName ensures the name is valid for Docker container/compose project names.
// Deprecated: Use common.SanitizeProjectName instead.
// This function is kept for backward compatibility and will be removed.
func SanitizeProjectName(name string) string {
	// Delegate to the canonical implementation in the common package
	// to avoid code duplication while maintaining backward compatibility.
	// This will be removed when all consumers are updated.
	if name == "" {
		return ""
	}

	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace spaces with underscores and filter invalid characters
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		} else if r == ' ' {
			result.WriteRune('_')
		}
		// Skip other characters
	}

	sanitized := result.String()
	if sanitized == "" {
		return ""
	}

	// Ensure starts with a letter (Docker requirement)
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "dcx_" + sanitized
	}

	return sanitized
}

// ImageExists checks if an image exists locally.
func (c *DockerClient) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	_, err := c.cli.ImageInspect(ctx, imageRef)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetImageLabels returns the labels for an image.
func (c *DockerClient) GetImageLabels(ctx context.Context, imageRef string) (map[string]string, error) {
	info, err := c.cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}
	if info.Config == nil {
		return nil, nil
	}
	return info.Config.Labels, nil
}

// GetImageID returns the ID of an image.
func (c *DockerClient) GetImageID(ctx context.Context, imageRef string) (string, error) {
	info, err := c.cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image: %w", err)
	}
	return info.ID, nil
}

// PullImageWithProgress pulls an image with optional progress display.
func (c *DockerClient) PullImageWithProgress(ctx context.Context, imageRef string, progressOut io.Writer) error {
	reader, err := c.cli.ImagePull(ctx, imageRef, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer func() { _ = reader.Close() }()

	if progressOut == nil {
		_, err = io.Copy(io.Discard, reader)
		return err
	}

	return processPullOutput(reader, progressOut)
}

// processPullOutput processes Docker pull progress output.
func processPullOutput(reader io.Reader, out io.Writer) error {
	type progressDetail struct {
		Current int64 `json:"current,omitempty"`
		Total   int64 `json:"total,omitempty"`
	}
	type progressEvent struct {
		ID             string         `json:"id,omitempty"`
		Status         string         `json:"status,omitempty"`
		Progress       string         `json:"progress,omitempty"`
		ProgressDetail progressDetail `json:"progressDetail,omitempty"`
		Stream         string         `json:"stream,omitempty"`
		Error          string         `json:"error,omitempty"`
	}

	decoder := json.NewDecoder(reader)
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
			_, _ = fmt.Fprintf(out, "%s\n", status)
			lastStatus = status
		}
	}

	return nil
}

// StartContainer starts a stopped container.
func (c *DockerClient) StartContainer(ctx context.Context, containerID string) error {
	return c.cli.ContainerStart(ctx, containerID, container.StartOptions{})
}

// StopContainer stops a running container.
func (c *DockerClient) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	var timeoutSecs *int
	if timeout != nil {
		secs := int(timeout.Seconds())
		timeoutSecs = &secs
	}
	return c.cli.ContainerStop(ctx, containerID, container.StopOptions{Timeout: timeoutSecs})
}

// RemoveContainer removes a container.
func (c *DockerClient) RemoveContainer(ctx context.Context, containerID string, force, removeVolumes bool) error {
	return c.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         force,
		RemoveVolumes: removeVolumes,
	})
}

// KillContainer sends a signal to a container.
func (c *DockerClient) KillContainer(ctx context.Context, containerID, signal string) error {
	return c.cli.ContainerKill(ctx, containerID, signal)
}

// CreateContainerOptions contains options for creating a container.
type CreateContainerOptions struct {
	Name            string
	Image           string
	WorkspacePath   string
	WorkspaceFolder string
	WorkspaceMount  string
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
	Ports           []string
	Entrypoint      []string
	Cmd             []string
	GPURequest      string // GPU request: "all" or count like "1", "2"
}

// CreateContainer creates a new container.
func (c *DockerClient) CreateContainer(ctx context.Context, opts CreateContainerOptions) (string, error) {
	hostConfig := &container.HostConfig{
		Privileged:  opts.Privileged,
		Init:        &opts.Init,
		CapAdd:      opts.CapAdd,
		CapDrop:     opts.CapDrop,
		SecurityOpt: opts.SecurityOpt,
		ExtraHosts:  opts.ExtraHosts,
		Sysctls:     opts.Sysctls,
	}

	if opts.NetworkMode != "" {
		hostConfig.NetworkMode = container.NetworkMode(opts.NetworkMode)
	}
	if opts.IpcMode != "" {
		hostConfig.IpcMode = container.IpcMode(opts.IpcMode)
	}
	if opts.PidMode != "" {
		hostConfig.PidMode = container.PidMode(opts.PidMode)
	}
	if opts.ShmSize > 0 {
		hostConfig.ShmSize = opts.ShmSize
	}

	// Handle GPU request (per devcontainer spec hostRequirements.gpu)
	// Empty Driver uses the default GPU runtime (nvidia, amd, etc.)
	if opts.GPURequest != "" {
		count := -1 // -1 means all GPUs
		if opts.GPURequest != "all" {
			if n, err := strconv.Atoi(opts.GPURequest); err == nil && n > 0 {
				count = n
			}
		}
		hostConfig.DeviceRequests = append(hostConfig.DeviceRequests, container.DeviceRequest{
			Count:        count,
			Capabilities: [][]string{{"gpu"}},
		})
	}

	for _, device := range opts.Devices {
		hostConfig.Devices = append(hostConfig.Devices, container.DeviceMapping{
			PathOnHost:        device,
			PathInContainer:   device,
			CgroupPermissions: "rwm",
		})
	}

	if len(opts.Tmpfs) > 0 {
		hostConfig.Tmpfs = opts.Tmpfs
	}

	if opts.WorkspaceMount != "" {
		bind := parseMountSpec(opts.WorkspaceMount)
		if bind != "" {
			hostConfig.Binds = append(hostConfig.Binds, bind)
		}
	} else if opts.WorkspacePath != "" && opts.WorkspaceFolder != "" {
		hostConfig.Binds = append(hostConfig.Binds, fmt.Sprintf("%s:%s", opts.WorkspacePath, opts.WorkspaceFolder))
	}

	if opts.SSHMountPath != "" {
		if common.IsDockerDesktop() {
			hostConfig.Binds = append(hostConfig.Binds, fmt.Sprintf("%s:%s:ro", opts.SSHMountPath, opts.SSHMountPath))
		} else {
			hostConfig.Binds = append(hostConfig.Binds, fmt.Sprintf("%s:/ssh-agent:ro", opts.SSHMountPath))
		}
	}

	hostConfig.Binds = append(hostConfig.Binds, opts.Mounts...)

	exposedPorts, portBindings := parsePortBindings(opts.Ports)
	if len(portBindings) > 0 {
		hostConfig.PortBindings = portBindings
	}

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

	if len(opts.Entrypoint) > 0 {
		containerConfig.Entrypoint = opts.Entrypoint
	}
	if len(opts.Cmd) > 0 {
		containerConfig.Cmd = opts.Cmd
	}

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
	ConfigDir  string
	Stdout     io.Writer
	Stderr     io.Writer
}

// BuildImage builds a Docker image from a Dockerfile.
func (c *DockerClient) BuildImage(ctx context.Context, opts ImageBuildOptions) error {
	configDir := opts.ConfigDir
	if configDir == "" {
		configDir = "."
	}

	contextPath := opts.Context
	if contextPath == "" {
		contextPath = configDir
	} else if !filepath.IsAbs(contextPath) {
		contextPath = filepath.Join(configDir, contextPath)
	}

	args := []string{"build"}

	if opts.Tag != "" {
		args = append(args, "-t", opts.Tag)
	}

	if opts.Dockerfile != "" {
		dockerfilePath := opts.Dockerfile
		if !filepath.IsAbs(dockerfilePath) {
			dockerfilePath = filepath.Join(configDir, dockerfilePath)
		}
		args = append(args, "-f", dockerfilePath)
	}

	if opts.Target != "" {
		args = append(args, "--target", opts.Target)
	}

	for key, value := range opts.Args {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	for _, cache := range opts.CacheFrom {
		args = append(args, "--cache-from", cache)
	}

	if common.IsSSHAgentAvailable() {
		args = append(args, "--ssh", "default")
	}

	args = append(args, contextPath)

	cmd := exec.CommandContext(ctx, "docker", args...)
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

// BuildImageCLI builds a Docker image using the CLI.
// This is the canonical function for all docker build operations.
// It can be called without a DockerClient instance.
func BuildImageCLI(ctx context.Context, opts ImageBuildOptions) error {
	configDir := opts.ConfigDir
	if configDir == "" {
		configDir = "."
	}

	contextPath := opts.Context
	if contextPath == "" {
		contextPath = configDir
	} else if !filepath.IsAbs(contextPath) {
		contextPath = filepath.Join(configDir, contextPath)
	}

	args := []string{"build"}

	if opts.Tag != "" {
		args = append(args, "-t", opts.Tag)
	}

	if opts.Dockerfile != "" {
		dockerfilePath := opts.Dockerfile
		if !filepath.IsAbs(dockerfilePath) {
			dockerfilePath = filepath.Join(configDir, dockerfilePath)
		}
		args = append(args, "-f", dockerfilePath)
	}

	if opts.Target != "" {
		args = append(args, "--target", opts.Target)
	}

	for key, value := range opts.Args {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", key, value))
	}

	for _, cache := range opts.CacheFrom {
		args = append(args, "--cache-from", cache)
	}

	if common.IsSSHAgentAvailable() {
		args = append(args, "--ssh", "default")
	}

	args = append(args, contextPath)

	cmd := exec.CommandContext(ctx, "docker", args...)
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

// parsePortBindings parses port specifications into exposed ports and port bindings.
func parsePortBindings(ports []string) (nat.PortSet, nat.PortMap) {
	bindings := parse.ParsePortBindings(ports)

	exposedPorts := make(nat.PortSet)
	portBindings := make(nat.PortMap)

	for _, pb := range bindings {
		natPort := nat.Port(fmt.Sprintf("%s/%s", pb.ContainerPort, pb.Protocol))
		exposedPorts[natPort] = struct{}{}
		portBindings[natPort] = []nat.PortBinding{
			{
				HostIP:   pb.HostIP,
				HostPort: pb.HostPort,
			},
		}
	}

	return exposedPorts, portBindings
}

// parseMountSpec parses a Docker --mount format string into a bind mount string.
func parseMountSpec(spec string) string {
	m := parse.ParseMount(spec)
	if m == nil {
		return ""
	}

	result := m.Source + ":" + m.Target
	var opts []string
	if m.ReadOnly {
		opts = append(opts, "ro")
	}
	if m.Consistency != "" {
		opts = append(opts, m.Consistency)
	}
	if len(opts) > 0 {
		result += ":" + strings.Join(opts, ",")
	}
	return result
}

// CleanupResult contains statistics about cleaned up resources.
type CleanupResult struct {
	ImagesRemoved  int
	SpaceReclaimed int64
}

// CleanupDerivedImages removes derived images created by dcx.
// If workspaceID is provided, only images for that environment are removed.
// If keepCurrent is true, the current derived image (matching configHash) is preserved.
func (c *DockerClient) CleanupDerivedImages(ctx context.Context, workspaceID, currentConfigHash string, keepCurrent bool) (*CleanupResult, error) {
	result := &CleanupResult{}

	images, err := c.cli.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			// Derived images follow the pattern: dcx-derived/<workspaceID>:<hash>
			if !strings.HasPrefix(tag, "dcx-derived/") {
				continue
			}

			// Parse the tag
			parts := strings.SplitN(strings.TrimPrefix(tag, "dcx-derived/"), ":", 2)
			if len(parts) != 2 {
				continue
			}

			imageWorkspaceID := parts[0]
			imageHash := parts[1]

			// If workspaceID filter is provided, only match that environment
			if workspaceID != "" && imageWorkspaceID != workspaceID {
				continue
			}

			// If keepCurrent is true and this is the current image, skip it
			if keepCurrent && currentConfigHash != "" && imageHash == currentConfigHash {
				continue
			}

			// Remove the image
			_, err := c.cli.ImageRemove(ctx, img.ID, image.RemoveOptions{
				Force:         false,
				PruneChildren: true,
			})
			if err != nil {
				// Log but continue - image might be in use
				continue
			}

			result.ImagesRemoved++
			result.SpaceReclaimed += img.Size
		}
	}

	return result, nil
}

// CleanupAllDerivedImages removes all derived images created by dcx.
func (c *DockerClient) CleanupAllDerivedImages(ctx context.Context) (*CleanupResult, error) {
	return c.CleanupDerivedImages(ctx, "", "", false)
}

// CleanupDanglingImages removes dangling (untagged) images.
func (c *DockerClient) CleanupDanglingImages(ctx context.Context) (*CleanupResult, error) {
	result := &CleanupResult{}

	filterArgs := filters.NewArgs()
	filterArgs.Add("dangling", "true")

	images, err := c.cli.ImageList(ctx, image.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list dangling images: %w", err)
	}

	for _, img := range images {
		_, err := c.cli.ImageRemove(ctx, img.ID, image.RemoveOptions{
			Force:         false,
			PruneChildren: true,
		})
		if err != nil {
			continue
		}

		result.ImagesRemoved++
		result.SpaceReclaimed += img.Size
	}

	return result, nil
}

// GetDerivedImageStats returns statistics about derived images.
func (c *DockerClient) GetDerivedImageStats(ctx context.Context) (count int, totalSize int64, err error) {
	images, err := c.cli.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list images: %w", err)
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if strings.HasPrefix(tag, "dcx-derived/") {
				count++
				totalSize += img.Size
				break
			}
		}
	}

	return count, totalSize, nil
}

// LogsOptions contains options for retrieving container logs.
type LogsOptions struct {
	Follow     bool
	Timestamps bool
	Tail       string // Number of lines or "all"
}

// GetLogs retrieves logs from a container.
func (c *DockerClient) GetLogs(ctx context.Context, containerID string, opts LogsOptions) (io.ReadCloser, error) {
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
