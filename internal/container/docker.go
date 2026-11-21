// Package container is dcx's container engine layer: the Engine interface,
// its Docker CLI implementation, and the runtime that builds images and
// creates the environment from a ContainerSpec.
package container

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/griffithind/dcx/internal/build"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/shell"
	"github.com/griffithind/dcx/internal/state"
)

// Docker wraps the Docker CLI with dcx-specific functionality.
// All operations use the Docker CLI for reliability and simplicity.
type Docker struct {
	// trace, when set, receives every docker command line before it runs
	// (dcx -v).
	trace func(cmdline string)

	// buildx records whether the buildx plugin is installed (asked once).
	buildxOnce sync.Once
	buildx     bool
}

// WithTrace makes d report every docker command line to trace before
// running it, shell-quoted so it can be copied and rerun.
func (d *Docker) WithTrace(trace func(cmdline string)) *Docker {
	d.trace = trace
	return d
}

// command builds a docker invocation, reporting it to the trace.
func (d *Docker) command(ctx context.Context, args ...string) *exec.Cmd {
	if d.trace != nil {
		d.trace(shell.Join(append([]string{"docker"}, args...)))
	}
	return exec.CommandContext(ctx, "docker", args...)
}

// NewDocker returns a Docker CLI engine after checking that the daemon is
// reachable.
func NewDocker() (*Docker, error) {
	d := &Docker{}
	if _, err := runOutput(d.command(context.Background(), "version", "--format", "{{.Server.Version}}")); err != nil {
		return nil, fmt.Errorf("docker not accessible: %w", err)
	}
	return d, nil
}

// NewDockerCLI returns a Docker CLI engine without contacting the daemon.
// Commands that may not need Docker at all (config, plan, lock) use it so
// they keep working when Docker is unavailable; operations report errors
// when they are actually attempted.
func NewDockerCLI() *Docker {
	return &Docker{}
}

// SystemInfo contains information about the Docker daemon's resources.
type SystemInfo struct {
	NCPU         int    // Number of CPUs available to Docker
	MemTotal     uint64 // Total memory available to Docker in bytes
	OSType       string // Operating system type (linux, windows)
	Architecture string // Architecture (x86_64, arm64, etc.)
	// GPU reports whether containers can be given GPUs (`--gpus`): the
	// NVIDIA container runtime is registered with the daemon.
	GPU bool
	// ServerVersion is the daemon's version.
	ServerVersion string
	// SELinux reports whether the daemon labels containers for SELinux
	// (its security options include selinux). Bind mounts then need
	// relabelling to be readable inside the container.
	SELinux bool
}

// Info returns system-wide information about Docker.
// This reflects Docker's configured resource limits, which may be less than the host's
// actual resources (e.g., Docker Desktop VM limits, cgroup limits).
func (d *Docker) Info(ctx context.Context) (*SystemInfo, error) {
	cmd := d.command(ctx, "info", "--format", "json")
	output, err := runOutput(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to get Docker info: %w", err)
	}

	var info struct {
		NCPU         int                        `json:"NCPU"`
		MemTotal     int64                      `json:"MemTotal"`
		OSType       string                     `json:"OSType"`
		Architecture string                     `json:"Architecture"`
		Runtimes     map[string]json.RawMessage `json:"Runtimes"`
		Version      string                     `json:"ServerVersion"`
		Security     []string                   `json:"SecurityOptions"`
	}
	if err := json.Unmarshal(output, &info); err != nil {
		return nil, fmt.Errorf("failed to parse Docker info: %w", err)
	}

	return &SystemInfo{
		NCPU:          info.NCPU,
		MemTotal:      uint64(info.MemTotal),
		OSType:        info.OSType,
		Architecture:  info.Architecture,
		GPU:           info.Runtimes["nvidia"] != nil,
		ServerVersion: info.Version,
		SELinux: slices.ContainsFunc(info.Security, func(opt string) bool {
			return opt == "name=selinux" || strings.HasPrefix(opt, "name=selinux,")
		}),
	}, nil
}

// ListContainersWithLabels returns containers matching label filters.
// Implements state.ContainerClient.
//
// Labels are read from `docker inspect` JSON. The `docker ps` Labels column
// is a comma-joined string that cannot be split reliably: dcx stores JSON
// (features.config) and filesystem paths, both of which may contain commas.
func (d *Docker) ListContainersWithLabels(ctx context.Context, labels map[string]string) ([]state.ContainerSummary, error) {
	args := []string{"ps", "-a", "-q", "--no-trunc"}
	for k, v := range labels {
		args = append(args, "--filter", fmt.Sprintf("label=%s=%s", k, v))
	}

	output, err := runOutput(d.command(ctx, args...))
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	ids := strings.Fields(string(output))
	if len(ids) == 0 {
		return nil, nil
	}

	inspectArgs := append([]string{"inspect", "--type", "container", "--format", "json"}, ids...)
	inspectOut, inspectErr := runOutput(d.command(ctx, inspectArgs...))
	summaries, parseErr := parseContainerSummaries(inspectOut)
	if parseErr != nil {
		if inspectErr != nil {
			return nil, fmt.Errorf("failed to inspect containers: %w", inspectErr)
		}
		return nil, parseErr
	}
	// A container removed between `ps` and `inspect` makes inspect exit
	// non-zero while still printing the others; that is not an error here.
	return summaries, nil
}

// parseContainerSummaries parses `docker inspect --format json` output.
func parseContainerSummaries(data []byte) ([]state.ContainerSummary, error) {
	var raw []struct {
		ID      string    `json:"Id"`
		Name    string    `json:"Name"`
		Created time.Time `json:"Created"`
		State   struct {
			Status  string `json:"Status"`
			Running bool   `json:"Running"`
		} `json:"State"`
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse container inspect output: %w", err)
	}

	result := make([]state.ContainerSummary, 0, len(raw))
	for _, c := range raw {
		labels := c.Config.Labels
		if labels == nil {
			labels = map[string]string{}
		}
		result = append(result, state.ContainerSummary{
			ID:      c.ID,
			Name:    strings.TrimPrefix(c.Name, "/"),
			State:   c.State.Status,
			Running: c.State.Running,
			Labels:  labels,
			Created: c.Created,
		})
	}
	return result, nil
}

// InspectContainer returns detailed information about a container.
// Implements state.ContainerClient.
func (d *Docker) InspectContainer(ctx context.Context, containerID string) (*state.ContainerDetails, error) {
	cmd := d.command(ctx, "inspect", "--format", "json", containerID)
	output, err := runOutput(cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	var results []struct {
		ID      string    `json:"Id"`
		Name    string    `json:"Name"`
		Created time.Time `json:"Created"`
		State   struct {
			Status    string `json:"Status"`
			Running   bool   `json:"Running"`
			StartedAt string `json:"StartedAt"`
		} `json:"State"`
		Image  string `json:"Image"`
		Config struct {
			Labels     map[string]string `json:"Labels"`
			WorkingDir string            `json:"WorkingDir"`
			Env        []string          `json:"Env"`
		} `json:"Config"`
		Mounts []struct {
			Type        string `json:"Type"`
			Name        string `json:"Name"`
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
	var volumes []string
	for i, m := range info.Mounts {
		mounts[i] = fmt.Sprintf("%s:%s", m.Source, m.Destination)
		if m.Type == "volume" && m.Name != "" && !anonymousVolume.MatchString(m.Name) {
			volumes = append(volumes, m.Name)
		}
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
		Volumes:    volumes,
		WorkingDir: info.Config.WorkingDir,
		Env:        info.Config.Env,
	}, nil
}

// Ensure Docker implements state.ContainerClient.
var _ state.ContainerClient = (*Docker)(nil)

// PortMapping returns the ephemeral host port Docker mapped to the given
// container port/proto (e.g. (48022, "tcp") → 32769).
//
// Returns an error if the port is not published or the container is gone.
func (d *Docker) PortMapping(ctx context.Context, containerName string, containerPort int, proto string) (int, error) {
	target := fmt.Sprintf("%d/%s", containerPort, proto)
	cmd := d.command(ctx, "port", containerName, target)
	output, err := runOutput(cmd)
	if err != nil {
		return 0, fmt.Errorf("docker port %s %s: %w", containerName, target, err)
	}
	// Output looks like one of:
	//   127.0.0.1:32769
	//   0.0.0.0:32769\n[::]:32769
	// Take the first line's last colon-separated component.
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return 0, fmt.Errorf("no port mapping for %s in %s", target, containerName)
	}
	line := strings.TrimSpace(lines[0])
	idx := strings.LastIndex(line, ":")
	if idx < 0 {
		return 0, fmt.Errorf("unexpected docker port output: %q", line)
	}
	portStr := line[idx+1:]
	var port int
	for _, r := range portStr {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("unexpected docker port output: %q", line)
		}
		port = port*10 + int(r-'0')
	}
	if port == 0 {
		return 0, fmt.Errorf("no host port bound to %s in %s", target, containerName)
	}
	return port, nil
}

// ImageExists checks if an image exists locally.
func (d *Docker) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	cmd := d.command(ctx, "image", "inspect", imageRef)
	if err := cmd.Run(); err != nil {
		// Exit code 1 means image not found
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ImageLabels returns the labels for an image.
// ImageConfig returns an image's USER, ENTRYPOINT and CMD.
func (d *Docker) ImageConfig(ctx context.Context, imageRef string) (build.ImageConfig, error) {
	output, err := runOutput(d.command(ctx, "image", "inspect", "--format", "json", imageRef))
	if err != nil {
		return build.ImageConfig{}, fmt.Errorf("failed to inspect image: %w", err)
	}
	var results []struct {
		Config struct {
			User       string   `json:"User"`
			Entrypoint []string `json:"Entrypoint"`
			Cmd        []string `json:"Cmd"`
		} `json:"Config"`
	}
	if err := json.Unmarshal(output, &results); err != nil {
		return build.ImageConfig{}, fmt.Errorf("failed to parse image inspect output: %w", err)
	}
	if len(results) == 0 {
		return build.ImageConfig{}, fmt.Errorf("image not found: %s", imageRef)
	}
	c := results[0].Config
	return build.ImageConfig{User: c.User, Entrypoint: c.Entrypoint, Cmd: c.Cmd}, nil
}

func (d *Docker) ImageLabels(ctx context.Context, imageRef string) (map[string]string, error) {
	cmd := d.command(ctx, "image", "inspect", "--format", "json", imageRef)
	output, err := runOutput(cmd)
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

// ImageID returns the ID of an image.
func (d *Docker) ImageID(ctx context.Context, imageRef string) (string, error) {
	cmd := d.command(ctx, "image", "inspect", "--format", "{{.Id}}", imageRef)
	output, err := runOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// PullImageWithProgress pulls an image with optional progress display.
func (d *Docker) PullImageWithProgress(ctx context.Context, imageRef string, progressOut io.Writer) error {
	cmd := d.command(ctx, "pull", imageRef)
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
	cmd := d.command(ctx, "start", containerID)
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

	cmd := d.command(ctx, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop container: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// anonymousVolume matches the generated names of anonymous volumes, which
// `docker rm -v` removes with their container.
var anonymousVolume = regexp.MustCompile(`^[0-9a-f]{64}$`)

// RemoveVolume removes a named volume.
func (d *Docker) RemoveVolume(ctx context.Context, name string) error {
	if _, err := runOutput(d.command(ctx, "volume", "rm", name)); err != nil {
		return fmt.Errorf("remove volume %s: %w", name, err)
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

	cmd := d.command(ctx, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove container: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// CreateContainer creates (but does not start) the container described by
// spec and returns its ID.
func (d *Docker) CreateContainer(ctx context.Context, spec ContainerSpec) (string, error) {
	cmd := d.command(ctx, dockerRunArgs(spec)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := runOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("failed to create container: %s", strings.TrimSpace(stderr.String()))
	}
	// Only stdout carries the ID. Docker prints warnings (platform
	// mismatch, discarded published ports, ...) on stderr, which used to be
	// read as part of the ID.
	id := lastLine(string(stdout))
	if id == "" {
		return "", fmt.Errorf("failed to create container: docker printed no container ID: %s", strings.TrimSpace(stderr.String()))
	}
	return id, nil
}

// lastLine returns the last non-empty line of s, trimmed.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// dockerRunArgs renders a ContainerSpec as `docker create` arguments.
func dockerRunArgs(s ContainerSpec) []string {
	// No -t, -i or -w, as devcontainers/cli creates it: an image command
	// that needs a terminal (bash) exits as it does there.
	args := []string{"create"}
	add := func(flag string, values ...string) {
		for _, v := range values {
			args = append(args, flag, v)
		}
	}

	if s.Name != "" {
		add("--name", s.Name)
	}
	if s.User != "" {
		add("-u", s.User)
	}
	if s.Privileged {
		args = append(args, "--privileged")
	}
	if s.Init {
		args = append(args, "--init")
	}
	if s.NetworkMode != "" {
		add("--network", s.NetworkMode)
	}
	if s.IpcMode != "" {
		add("--ipc", s.IpcMode)
	}
	if s.PidMode != "" {
		add("--pid", s.PidMode)
	}
	if s.ShmSize > 0 {
		add("--shm-size", strconv.FormatInt(s.ShmSize, 10))
	}
	add("--cap-add", s.CapAdd...)
	add("--cap-drop", s.CapDrop...)
	add("--security-opt", s.SecurityOpt...)
	add("--device", s.Devices...)
	add("--add-host", s.ExtraHosts...)
	for _, k := range sortedKeys(s.Sysctls) {
		add("--sysctl", k+"="+s.Sysctls[k])
	}
	for _, path := range sortedKeys(s.Tmpfs) {
		if opts := s.Tmpfs[path]; opts != "" {
			add("--tmpfs", path+":"+opts)
		} else {
			add("--tmpfs", path)
		}
	}
	if s.GPUs != "" {
		add("--gpus", s.GPUs)
	}
	for _, p := range s.Ports {
		add("-p", p.DockerSpec())
	}
	for i := range s.Mounts {
		if m := &s.Mounts[i]; m.Relabel {
			// --mount cannot relabel; the -v form can (":z").
			add("-v", relabelVolume(m))
		} else {
			add("--mount", formatMount(m))
		}
	}
	for _, k := range sortedKeys(s.Labels) {
		add("-l", k+"="+s.Labels[k])
	}
	for _, k := range sortedKeys(s.Env) {
		add("-e", k+"="+s.Env[k])
	}

	// Remaining runArgs from devcontainer.json, verbatim. They come after
	// dcx's own flags so single-valued options (e.g. --hostname) win.
	args = append(args, s.RunArgs...)

	if len(s.Entrypoint) > 0 {
		add("--entrypoint", s.Entrypoint[0])
	}
	args = append(args, s.Image)
	// docker takes only the entrypoint executable as a flag; the rest of
	// the entrypoint, then Cmd, become the arguments (as compose does with
	// entrypoint + command).
	if len(s.Entrypoint) > 1 {
		args = append(args, s.Entrypoint[1:]...)
	}
	return append(args, s.Cmd...)
}

// formatMount formats a devcontainer.Mount as a --mount flag value.
func formatMount(m *devcontainer.Mount) string {
	mountType := m.Type
	if mountType == "" {
		mountType = "bind"
	}
	parts := []string{"type=" + mountType}
	if m.Source != "" {
		parts = append(parts, "source="+m.Source)
	}
	if m.Target != "" {
		parts = append(parts, "target="+m.Target)
	}
	if m.ReadOnly {
		parts = append(parts, "readonly")
	}
	if m.Consistency != "" {
		parts = append(parts, "consistency="+m.Consistency)
	}
	parts = append(parts, m.Options...)
	// --mount is CSV: a field containing a comma or quote (a path such as
	// /home/u/a,b) is quoted, or Docker splits it.
	for i, p := range parts {
		if strings.ContainsAny(p, ",\"") {
			parts[i] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
		}
	}
	return strings.Join(parts, ",")
}

// relabelVolume renders a bind mount as a -v value with SELinux
// relabelling ("source:target[:ro],z").
func relabelVolume(m *devcontainer.Mount) string {
	opts := "z"
	if m.ReadOnly {
		opts = "ro,z"
	}
	return m.Source + ":" + m.Target + ":" + opts
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

	cmd := d.command(ctx, args...)
	output, err := runOutput(cmd)
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
	cmd := d.command(ctx, "rmi", imageID)
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

// isDcxImage reports whether an image was built by dcx: everything under
// the dcx/ repository (dcx/<workspace>:base, :features-*, :uid-*), plus the
// dcx-derived* names older versions used.
func isDcxImage(img imageInfo) bool {
	return strings.HasPrefix(img.Repository, ImageTagPrefix) ||
		strings.HasPrefix(img.Repository, "dcx-derived")
}

// CleanupDerivedImages removes images built by dcx. Images still used by a
// container (running or stopped) are kept: docker refuses to remove them
// without --force, which is never passed.
//
// workspaceID limits the cleanup to one workspace's images
// (dcx/<workspaceID>:*); "" cleans every dcx image.
func (d *Docker) CleanupDerivedImages(ctx context.Context, workspaceID string) (*CleanupResult, error) {
	result := &CleanupResult{}

	images, err := d.listImages(ctx)
	if err != nil {
		return nil, err
	}

	for _, img := range images {
		if !isDcxImage(img) {
			continue
		}
		if workspaceID != "" && img.Repository != ImageTagPrefix+workspaceID {
			continue
		}
		// Remove by reference so an image with several dcx tags is untagged
		// tag by tag instead of failing with "referenced in multiple
		// repositories".
		ref := img.Repository + ":" + img.Tag
		if img.Tag == "" || img.Tag == "<none>" {
			ref = img.ID
		}
		if err := d.removeImage(ctx, ref); err != nil {
			continue // in use
		}
		result.ImagesRemoved++
		result.SpaceReclaimed += parseImageSize(img.Size)
	}

	return result, nil
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

// DerivedImageStats returns statistics about derived images.
func (d *Docker) DerivedImageStats(ctx context.Context) (count int, totalSize int64, err error) {
	images, err := d.listImages(ctx)
	if err != nil {
		return 0, 0, err
	}

	for _, img := range images {
		if isDcxImage(img) {
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

// Logs retrieves logs from a container.
func (d *Docker) Logs(ctx context.Context, containerID string, opts LogsOptions) (io.ReadCloser, error) {
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

	cmd := d.command(ctx, args...)
	pr, pw := io.Pipe()
	// Both streams go to the reader: `docker logs` replays the container's
	// stderr on its own stderr. A failure (no such container, daemon gone)
	// surfaces as the reader's error instead of looking like empty logs.
	cmd.Stdout = pw
	cmd.Stderr = pw

	go func() {
		if err := cmd.Run(); err != nil {
			_ = pw.CloseWithError(fmt.Errorf("docker logs: %w", err))
			return
		}
		_ = pw.Close()
	}()

	return pr, nil
}

// ExecInContainer runs a command in a container and returns non-nil error
// if it exits non-zero. Used by host-side callers that only care about
// exit status (e.g. liveness probes).
func (d *Docker) ExecInContainer(ctx context.Context, containerName string, argv []string) error {
	args := append([]string{"exec", containerName}, argv...)
	cmd := d.command(ctx, args...)
	return cmd.Run()
}

// ExecDetachedAsRoot starts a command in a container as root, as a
// background process (`docker exec -d`). Docker returns once the command
// has been spawned; its lifecycle is owned by the container.
func (d *Docker) ExecDetachedAsRoot(ctx context.Context, containerName string, argv []string) error {
	args := append([]string{"exec", "-d", "--user", "root", containerName}, argv...)
	cmd := d.command(ctx, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker exec -d failed: %w, output: %s", err, output)
	}
	return nil
}

// ExecAsRoot runs argv in the container as root and returns its combined
// output.
func (d *Docker) ExecAsRoot(ctx context.Context, containerName string, argv []string) ([]byte, error) {
	args := append([]string{"exec", "--user", "root", containerName}, argv...)
	out, err := d.command(ctx, args...).CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("docker exec as root failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// ExecOutput runs argv in the container (as its default user) and returns
// its stdout.
func (d *Docker) ExecOutput(ctx context.Context, containerName string, argv []string) ([]byte, error) {
	args := append([]string{"exec", containerName}, argv...)
	return runOutput(d.command(ctx, args...))
}

// ExecOutputAs runs argv in the container as user ("" = the container's
// default user) and returns its stdout. A non-zero exit is an error.
func (d *Docker) ExecOutputAs(ctx context.Context, containerName, user string, argv []string) ([]byte, error) {
	args := []string{"exec"}
	if user != "" {
		args = append(args, "--user", user)
	}
	args = append(append(args, containerName), argv...)
	return runOutput(d.command(ctx, args...))
}

// writeFileScript writes stdin to $1 as root: it creates the directories
// after $3 (mode 0755), then writes with umask 077 and applies the owner
// ($2) and mode ($3). Every value is a positional argument; nothing is
// interpolated into the script.
const writeFileScript = `set -e
f=$1 owner=$2 mode=$3
shift 3
for d in "$@"; do mkdir -p "$d"; chmod 0755 "$d"; done
umask 077
cat > "$f"
[ -z "$owner" ] || chown "$owner" "$f"
[ -z "$mode" ] || chmod "$mode" "$f"`

// WriteFileInContainer writes f inside a container with a single docker
// exec (docker cp cannot write to the /run/secrets tmpfs). The content
// goes through stdin, so it never appears in a command line.
func (d *Docker) WriteFileInContainer(ctx context.Context, containerName string, f FileWrite) error {
	args := append([]string{"exec", "-i", "--user", "root", containerName,
		"sh", "-c", writeFileScript, "dcx-write", f.Path, f.Owner, f.Mode}, f.Dirs...)
	cmd := d.command(ctx, args...)
	cmd.Stdin = bytes.NewReader(f.Content)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("write %s failed: %w, output: %s", f.Path, err, output)
	}
	return nil
}

// BuildImage runs `docker buildx build --load` for opts, or, without the
// buildx plugin, `docker build` with BuildKit (as devcontainers/cli falls
// back). Build contexts, which the features layer uses, need buildx.
// Paths in opts are absolute (build.Builder resolves them). Output streams
// to opts.Progress, or to dcx's stdout/stderr.
func (d *Docker) BuildImage(ctx context.Context, opts build.DockerfileBuildOptions) error {
	d.buildxOnce.Do(func() {
		d.buildx = d.command(ctx, "buildx", "version").Run() == nil
	})
	if !d.buildx && len(opts.BuildContexts) > 0 {
		return fmt.Errorf("building features needs the docker buildx plugin (BuildKit build contexts); install docker-buildx")
	}
	cmd := d.command(ctx, buildArgs(opts, d.buildx)...)
	if !d.buildx {
		cmd.Env = append(os.Environ(), "DOCKER_BUILDKIT=1")
	}
	if opts.Progress != nil {
		cmd.Stdout = opts.Progress
		cmd.Stderr = opts.Progress
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

// buildArgs renders build options as `docker buildx build` arguments (or
// `docker build` ones without buildx), with map-valued options sorted so
// the command line is deterministic.
func buildArgs(opts build.DockerfileBuildOptions, buildx bool) []string {
	args := []string{"build"}
	if buildx {
		args = []string{"buildx", "build"}
	}
	add := func(flag string, values ...string) {
		for _, v := range values {
			args = append(args, flag, v)
		}
	}
	if opts.Tag != "" {
		add("-t", opts.Tag)
	}
	add("-t", opts.ExtraTags...)
	if opts.Dockerfile != "" {
		add("-f", opts.Dockerfile)
	}
	for _, k := range sortedKeys(opts.Args) {
		add("--build-arg", k+"="+opts.Args[k])
	}
	if opts.Metadata != "" {
		add("--label", devcontainer.DevcontainerMetadataLabel+"="+opts.Metadata)
	}
	add("--cache-from", opts.CacheFrom...)
	if opts.NoCache {
		args = append(args, "--no-cache")
	}
	if opts.Pull {
		args = append(args, "--pull")
	}
	if opts.Target != "" {
		add("--target", opts.Target)
	}
	for _, name := range sortedKeys(opts.BuildContexts) {
		add("--build-context", name+"="+opts.BuildContexts[name])
	}
	for _, id := range sortedKeys(opts.Secrets) {
		add("--secret", "id="+id+",src="+opts.Secrets[id])
	}
	// build.options from devcontainer.json, verbatim.
	args = append(args, opts.Options...)
	if buildx {
		args = append(args, "--load")
	}
	args = append(args, opts.Context)
	return args
}

// CopyToContainer copies a host file into the container (`docker cp`).
func (d *Docker) CopyToContainer(ctx context.Context, src, containerName, dest string) error {
	out, err := d.command(ctx, "cp", src, containerName+":"+dest).CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker cp: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Compose runs `docker compose` for project p, streaming its output.
func (d *Docker) Compose(ctx context.Context, p ComposeProject, args ...string) error {
	cmd := d.command(ctx, p.args(args...)...)
	cmd.Dir = p.Dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ComposeOutput runs `docker compose` for project p and returns its stdout.
func (d *Docker) ComposeOutput(ctx context.Context, p ComposeProject, args ...string) ([]byte, error) {
	cmd := d.command(ctx, p.args(args...)...)
	cmd.Dir = p.Dir
	return runOutput(cmd)
}

// ComposeVersion returns the version of the docker compose plugin.
func (d *Docker) ComposeVersion(ctx context.Context) (string, error) {
	out, err := runOutput(d.command(ctx, "compose", "version", "--short"))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// runOutput runs cmd and returns its stdout. On failure the error is what
// docker printed on stderr ("exit status 1" alone explains nothing).
func runOutput(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if msg := strings.TrimSpace(string(exitErr.Stderr)); msg != "" {
			return out, &dockerError{msg: msg, err: err}
		}
	}
	return out, err
}

// dockerError is a failed docker command: the message is docker's
// stderr; it unwraps to the *exec.ExitError.
type dockerError struct {
	msg string
	err error
}

func (e *dockerError) Error() string { return e.msg }

func (e *dockerError) Unwrap() error { return e.err }

// DaemonPermissionHint explains a permission-denied error from the Docker
// socket: the daemon runs, but the user may not use it.
const DaemonPermissionHint = "the Docker daemon is running but your user may not use it: add it to the docker group (then log in again), or run dcx under 'sg docker'"

// IsDaemonPermissionDenied reports whether msg is docker's error for a
// socket the user may not open.
func IsDaemonPermissionDenied(msg string) bool {
	return strings.Contains(msg, "permission denied") && strings.Contains(msg, "docker")
}
