// Package container provides container runtime management for devcontainers.
// This package replaces the previous internal/docker and internal/runner packages
// with clearer naming (Docker, ContainerRuntime).
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
	"sync"
	"time"

	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/state"
)

// Docker wraps the Docker CLI with dcx-specific functionality.
// All operations use the Docker CLI for reliability and simplicity.
type Docker struct{}

// Singleton instance for Docker.
var (
	docker     *Docker
	dockerOnce sync.Once
	dockerErr  error
)

// NewDocker creates a new Docker client.
// Validates that Docker is accessible via the CLI.
func NewDocker() (*Docker, error) {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker not accessible: %w", err)
	}
	return &Docker{}, nil
}

// DockerClient returns the singleton Docker instance, validating Docker access on first use.
func DockerClient() (*Docker, error) {
	dockerOnce.Do(func() {
		docker, dockerErr = NewDocker()
	})
	return docker, dockerErr
}

// MustDocker returns the singleton Docker instance, panicking if Docker is not accessible.
func MustDocker() *Docker {
	d, err := DockerClient()
	if err != nil {
		panic(fmt.Sprintf("docker not accessible: %v", err))
	}
	return d
}

// Ping checks if the Docker daemon is accessible.
func (d *Docker) Ping(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "docker", "info")
	return cmd.Run()
}

// ServerVersion returns the Docker server version.
func (d *Docker) ServerVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get Docker version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
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
func (d *Docker) Info(ctx context.Context) (*SystemInfo, error) {
	cmd := exec.CommandContext(ctx, "docker", "info", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get Docker info: %w", err)
	}

	var info struct {
		NCPU         int    `json:"NCPU"`
		MemTotal     int64  `json:"MemTotal"`
		OSType       string `json:"OSType"`
		Architecture string `json:"Architecture"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to parse Docker info: %w", err)
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
func (d *Docker) ListContainersWithLabels(ctx context.Context, labels map[string]string) ([]state.ContainerSummary, error) {
	args := []string{"ps", "-a", "--format", "json", "--no-trunc"}
	for k, v := range labels {
		args = append(args, "--filter", fmt.Sprintf("label=%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Parse JSON lines (docker ps outputs one JSON object per line)
	var result []state.ContainerSummary
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var c struct {
			ID     string `json:"ID"`
			Names  string `json:"Names"`
			State  string `json:"State"`
			Labels string `json:"Labels"`
		}
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			continue // Skip malformed lines
		}

		// Parse labels from "key=value,key2=value2" format
		labelMap := make(map[string]string)
		if c.Labels != "" {
			for _, kv := range strings.Split(c.Labels, ",") {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) == 2 {
					labelMap[parts[0]] = parts[1]
				}
			}
		}

		result = append(result, state.ContainerSummary{
			ID:      c.ID,
			Name:    c.Names,
			State:   c.State,
			Running: c.State == "running",
			Labels:  labelMap,
		})
	}
	return result, nil
}

// InspectContainer returns detailed information about a container.
// Implements state.ContainerClient.
func (d *Docker) InspectContainer(ctx context.Context, containerID string) (*state.ContainerDetails, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "json", containerID)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	var results []struct {
		ID    string `json:"Id"`
		Name  string `json:"Name"`
		State struct {
			Status    string `json:"Status"`
			Running   bool   `json:"Running"`
			StartedAt string `json:"StartedAt"`
		} `json:"State"`
		Image  string `json:"Image"`
		Config struct {
			Labels     map[string]string `json:"Labels"`
			WorkingDir string            `json:"WorkingDir"`
		} `json:"Config"`
		Mounts []struct {
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
		} `json:"Mounts"`
	}

	if err := json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("failed to parse container inspect output: %w", err)
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("container not found: %s", containerID)
	}

	info := results[0]
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

// Ensure Docker implements state.ContainerClient.
var _ state.ContainerClient = (*Docker)(nil)

// ImageExists checks if an image exists locally.
func (d *Docker) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", imageRef)
	if err := cmd.Run(); err != nil {
		// Exit code 1 means image not found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetImageLabels returns the labels for an image.
func (d *Docker) GetImageLabels(ctx context.Context, imageRef string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "json", imageRef)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}

	var results []struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(output, &results); err != nil {
		return nil, fmt.Errorf("failed to parse image inspect output: %w", err)
	}

	if len(results) == 0 {
		return nil, nil
	}
	return results[0].Config.Labels, nil
}

// GetImageID returns the ID of an image.
func (d *Docker) GetImageID(ctx context.Context, imageRef string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "image", "inspect", "--format", "{{.Id}}", imageRef)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to inspect image: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// PullImageWithProgress pulls an image with optional progress display.
func (d *Docker) PullImageWithProgress(ctx context.Context, imageRef string, progressOut io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", "pull", imageRef)
	if progressOut != nil {
		cmd.Stdout = progressOut
		cmd.Stderr = progressOut
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	return nil
}

// StartContainer starts a stopped container using Docker CLI.
func (d *Docker) StartContainer(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, "docker", "start", containerID)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start container: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// StopContainer stops a running container using Docker CLI.
func (d *Docker) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	args := []string{"stop"}
	if timeout != nil {
		args = append(args, "-t", strconv.Itoa(int(timeout.Seconds())))
	}
	args = append(args, containerID)

	cmd := exec.CommandContext(ctx, "docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop container: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// RemoveContainer removes a container using Docker CLI.
func (d *Docker) RemoveContainer(ctx context.Context, containerID string, force, removeVolumes bool) error {
	args := []string{"rm"}
	if force {
		args = append(args, "-f")
	}
	if removeVolumes {
		args = append(args, "-v")
	}
	args = append(args, containerID)

	cmd := exec.CommandContext(ctx, "docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove container: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// KillContainer sends a signal to a container using Docker CLI.
func (d *Docker) KillContainer(ctx context.Context, containerID, signal string) error {
	args := []string{"kill"}
	if signal != "" {
		args = append(args, "-s", signal)
	}
	args = append(args, containerID)

	cmd := exec.CommandContext(ctx, "docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to kill container: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// CreateContainerOptions contains options for creating a container.
type CreateContainerOptions struct {
	Name            string
	Image           string
	WorkspacePath   string
	WorkspaceFolder string
	WorkspaceMount  *devcontainer.Mount // Structured workspace mount (nil means use WorkspacePath/WorkspaceFolder)
	Labels          map[string]string
	Env             []string
	Mounts          []devcontainer.Mount // Structured mount specifications
	RunArgs         []string
	User            string
	Privileged      bool
	Init            bool
	CapAdd      []string
	CapDrop     []string
	SecurityOpt []string
	NetworkMode string
	IpcMode         string
	PidMode         string
	ShmSize         int64
	Devices         []string
	ExtraHosts      []string
	Tmpfs           map[string]string
	Sysctls         map[string]string
	Ports           []devcontainer.PortForward // Structured port bindings
	Entrypoint      []string
	Cmd             []string
	GPURequest      string // GPU request: "all" or count like "1", "2"
}

// CreateContainer creates a new container using Docker CLI.
// Returns the container ID.
func (d *Docker) CreateContainer(ctx context.Context, opts CreateContainerOptions) (string, error) {
	args := []string{"run", "-d"}

	// Container name
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}

	// User
	if opts.User != "" {
		args = append(args, "-u", opts.User)
	}

	// Working directory
	if opts.WorkspaceFolder != "" {
		args = append(args, "-w", opts.WorkspaceFolder)
	}

	// TTY and stdin
	args = append(args, "-t", "-i")

	// Privileged mode
	if opts.Privileged {
		args = append(args, "--privileged")
	}

	// Init process
	if opts.Init {
		args = append(args, "--init")
	}

	// Network mode
	if opts.NetworkMode != "" {
		args = append(args, "--network", opts.NetworkMode)
	}

	// IPC mode
	if opts.IpcMode != "" {
		args = append(args, "--ipc", opts.IpcMode)
	}

	// PID mode
	if opts.PidMode != "" {
		args = append(args, "--pid", opts.PidMode)
	}

	// Shared memory size
	if opts.ShmSize > 0 {
		args = append(args, "--shm-size", strconv.FormatInt(opts.ShmSize, 10))
	}

	// Capabilities
	for _, cap := range opts.CapAdd {
		args = append(args, "--cap-add", cap)
	}
	for _, cap := range opts.CapDrop {
		args = append(args, "--cap-drop", cap)
	}

	// Security options
	for _, opt := range opts.SecurityOpt {
		args = append(args, "--security-opt", opt)
	}

	// Devices
	for _, device := range opts.Devices {
		args = append(args, "--device", device)
	}

	// Extra hosts
	for _, host := range opts.ExtraHosts {
		args = append(args, "--add-host", host)
	}

	// Sysctls
	for k, v := range opts.Sysctls {
		args = append(args, "--sysctl", fmt.Sprintf("%s=%s", k, v))
	}

	// Tmpfs mounts
	for path, opts := range opts.Tmpfs {
		if opts != "" {
			args = append(args, "--tmpfs", fmt.Sprintf("%s:%s", path, opts))
		} else {
			args = append(args, "--tmpfs", path)
		}
	}

	// GPU support
	if opts.GPURequest != "" {
		if opts.GPURequest == "all" {
			args = append(args, "--gpus", "all")
		} else {
			args = append(args, "--gpus", opts.GPURequest)
		}
	}

	// Port bindings - now much simpler!
	for _, p := range opts.Ports {
		hostPort := p.HostPort
		if hostPort == 0 {
			hostPort = p.ContainerPort
		}
		if p.Host != "" {
			args = append(args, "-p", fmt.Sprintf("%s:%d:%d", p.Host, hostPort, p.ContainerPort))
		} else {
			args = append(args, "-p", fmt.Sprintf("%d:%d", hostPort, p.ContainerPort))
		}
	}

	// Workspace mount
	if opts.WorkspaceMount != nil {
		mountStr := formatMount(opts.WorkspaceMount)
		args = append(args, "--mount", mountStr)
	} else if opts.WorkspacePath != "" && opts.WorkspaceFolder != "" {
		args = append(args, "-v", fmt.Sprintf("%s:%s", opts.WorkspacePath, opts.WorkspaceFolder))
	}

	// Additional mounts
	for _, m := range opts.Mounts {
		mountStr := formatMount(&m)
		args = append(args, "--mount", mountStr)
	}

	// Labels
	for k, v := range opts.Labels {
		args = append(args, "-l", fmt.Sprintf("%s=%s", k, v))
	}

	// Environment variables
	for _, e := range opts.Env {
		args = append(args, "-e", e)
	}

	// Entrypoint
	if len(opts.Entrypoint) > 0 {
		args = append(args, "--entrypoint", opts.Entrypoint[0])
	}

	// Image
	args = append(args, opts.Image)

	// Command (either remaining entrypoint args or cmd)
	if len(opts.Entrypoint) > 1 {
		args = append(args, opts.Entrypoint[1:]...)
	} else if len(opts.Cmd) > 0 {
		args = append(args, opts.Cmd...)
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to create container: %s", strings.TrimSpace(string(output)))
	}

	containerID := strings.TrimSpace(string(output))
	return containerID, nil
}

// formatMount formats a devcontainer.Mount as a --mount flag value.
func formatMount(m *devcontainer.Mount) string {
	mountType := m.Type
	if mountType == "" {
		mountType = "bind"
	}
	parts := []string{fmt.Sprintf("type=%s", mountType)}

	if m.Source != "" {
		parts = append(parts, fmt.Sprintf("source=%s", m.Source))
	}
	if m.Target != "" {
		parts = append(parts, fmt.Sprintf("target=%s", m.Target))
	}
	if m.ReadOnly {
		parts = append(parts, "readonly")
	}
	if m.Consistency != "" {
		parts = append(parts, fmt.Sprintf("consistency=%s", m.Consistency))
	}

	return strings.Join(parts, ",")
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
func (d *Docker) BuildImage(ctx context.Context, opts ImageBuildOptions) error {
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

// CleanupResult contains statistics about cleaned up resources.
type CleanupResult struct {
	ImagesRemoved  int
	SpaceReclaimed int64
}

// imageInfo holds parsed image information from docker images command.
type imageInfo struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	Size       string `json:"Size"`
}

// listImages lists all images using docker images command.
func (d *Docker) listImages(ctx context.Context, filters ...string) ([]imageInfo, error) {
	args := []string{"images", "--format", "json", "--no-trunc"}
	for _, f := range filters {
		args = append(args, "--filter", f)
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	var images []imageInfo
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var img imageInfo
		if err := json.Unmarshal([]byte(line), &img); err != nil {
			continue
		}
		images = append(images, img)
	}
	return images, nil
}

// removeImage removes an image by ID using docker rmi.
func (d *Docker) removeImage(ctx context.Context, imageID string) error {
	cmd := exec.CommandContext(ctx, "docker", "rmi", imageID)
	return cmd.Run()
}

// parseImageSize parses a human-readable size string to bytes.
func parseImageSize(sizeStr string) int64 {
	sizeStr = strings.TrimSpace(sizeStr)
	if sizeStr == "" {
		return 0
	}

	var multiplier int64 = 1
	sizeStr = strings.ToUpper(sizeStr)

	if strings.HasSuffix(sizeStr, "KB") {
		multiplier = 1024
		sizeStr = strings.TrimSuffix(sizeStr, "KB")
	} else if strings.HasSuffix(sizeStr, "MB") {
		multiplier = 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "MB")
	} else if strings.HasSuffix(sizeStr, "GB") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = strings.TrimSuffix(sizeStr, "GB")
	} else if strings.HasSuffix(sizeStr, "B") {
		sizeStr = strings.TrimSuffix(sizeStr, "B")
	}

	sizeStr = strings.TrimSpace(sizeStr)
	var size float64
	_, _ = fmt.Sscanf(sizeStr, "%f", &size)
	return int64(size * float64(multiplier))
}

// CleanupDerivedImages removes derived images created by dcx.
// If workspaceID is provided, only images for that environment are removed.
// If keepCurrent is true, the current derived image (matching configHash) is preserved.
func (d *Docker) CleanupDerivedImages(ctx context.Context, workspaceID, currentConfigHash string, keepCurrent bool) (*CleanupResult, error) {
	result := &CleanupResult{}

	images, err := d.listImages(ctx)
	if err != nil {
		return nil, err
	}

	for _, img := range images {
		// Derived images follow the pattern: dcx-derived/<workspaceID>:<hash>
		if img.Repository == "" || !strings.HasPrefix(img.Repository, "dcx-derived/") {
			continue
		}

		// Parse the tag
		imageWorkspaceID := strings.TrimPrefix(img.Repository, "dcx-derived/")
		imageHash := img.Tag

		// If workspaceID filter is provided, only match that environment
		if workspaceID != "" && imageWorkspaceID != workspaceID {
			continue
		}

		// If keepCurrent is true and this is the current image, skip it
		if keepCurrent && currentConfigHash != "" && imageHash == currentConfigHash {
			continue
		}

		// Remove the image
		if err := d.removeImage(ctx, img.ID); err != nil {
			// Log but continue - image might be in use
			continue
		}

		result.ImagesRemoved++
		result.SpaceReclaimed += parseImageSize(img.Size)
	}

	return result, nil
}

// CleanupAllDerivedImages removes all derived images created by dcx.
func (d *Docker) CleanupAllDerivedImages(ctx context.Context) (*CleanupResult, error) {
	return d.CleanupDerivedImages(ctx, "", "", false)
}

// CleanupDanglingImages removes dangling (untagged) images.
func (d *Docker) CleanupDanglingImages(ctx context.Context) (*CleanupResult, error) {
	result := &CleanupResult{}

	images, err := d.listImages(ctx, "dangling=true")
	if err != nil {
		return nil, fmt.Errorf("failed to list dangling images: %w", err)
	}

	for _, img := range images {
		if err := d.removeImage(ctx, img.ID); err != nil {
			continue
		}

		result.ImagesRemoved++
		result.SpaceReclaimed += parseImageSize(img.Size)
	}

	return result, nil
}

// GetDerivedImageStats returns statistics about derived images.
func (d *Docker) GetDerivedImageStats(ctx context.Context) (count int, totalSize int64, err error) {
	images, err := d.listImages(ctx)
	if err != nil {
		return 0, 0, err
	}

	for _, img := range images {
		if strings.HasPrefix(img.Repository, "dcx-derived/") {
			count++
			totalSize += parseImageSize(img.Size)
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
func (d *Docker) GetLogs(ctx context.Context, containerID string, opts LogsOptions) (io.ReadCloser, error) {
	args := []string{"logs"}
	if opts.Follow {
		args = append(args, "-f")
	}
	if opts.Timestamps {
		args = append(args, "-t")
	}
	if opts.Tail != "" && opts.Tail != "all" {
		args = append(args, "--tail", opts.Tail)
	}
	args = append(args, containerID)

	cmd := exec.CommandContext(ctx, "docker", args...)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	go func() {
		_ = cmd.Run()
		_ = pw.Close()
	}()

	return pr, nil
}

// SimpleExecOptions contains options for simple exec operations.
type SimpleExecOptions struct {
	User string
	Cmd  []string
}

// SimpleExecInContainer runs a command in a container and returns the combined output.
// This is a simplified version for internal use by helper operations.
func (d *Docker) SimpleExecInContainer(ctx context.Context, containerName string, opts SimpleExecOptions) ([]byte, error) {
	args := []string{"exec"}
	if opts.User != "" {
		args = append(args, "--user", opts.User)
	}
	args = append(args, containerName)
	args = append(args, opts.Cmd...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

// CopyToContainer copies a file to a container.
func (d *Docker) CopyToContainer(ctx context.Context, src, containerName, dest string) error {
	cmd := exec.CommandContext(ctx, "docker", "cp", src, containerName+":"+dest)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker cp failed: %w, output: %s", err, output)
	}
	return nil
}

// ChmodInContainer changes file permissions inside a container.
func (d *Docker) ChmodInContainer(ctx context.Context, containerName, path, mode, user string) error {
	args := []string{"exec"}
	if user != "" {
		args = append(args, "--user", user)
	}
	args = append(args, containerName, "chmod", mode, path)

	cmd := exec.CommandContext(ctx, "docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chmod failed: %w, output: %s", err, output)
	}
	return nil
}

// MkdirInContainer creates a directory inside a container.
func (d *Docker) MkdirInContainer(ctx context.Context, containerName, path, user string) error {
	args := []string{"exec"}
	if user != "" {
		args = append(args, "--user", user)
	}
	args = append(args, containerName, "mkdir", "-p", path)

	cmd := exec.CommandContext(ctx, "docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkdir failed: %w, output: %s", err, output)
	}
	return nil
}

// ChownInContainer changes file ownership inside a container.
func (d *Docker) ChownInContainer(ctx context.Context, containerName, path, owner string) error {
	args := []string{"exec", "--user", "root", containerName, "chown", owner, path}

	cmd := exec.CommandContext(ctx, "docker", args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chown failed: %w, output: %s", err, output)
	}
	return nil
}

// WriteFileInContainer writes content to a file inside a container using docker exec.
// This is useful for writing to tmpfs mounts where docker cp doesn't work.
func (d *Docker) WriteFileInContainer(ctx context.Context, containerName, path string, content []byte, user string) error {
	if user == "" {
		user = "root"
	}
	args := []string{"exec", "-i", "--user", user, containerName, "sh", "-c", fmt.Sprintf("cat > %q", path)}

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = strings.NewReader(string(content))

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("write file failed: %w, output: %s", err, output)
	}
	return nil
}
