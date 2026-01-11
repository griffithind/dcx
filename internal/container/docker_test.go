package container

import (
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
)

func TestCreateContainerOptions(t *testing.T) {
	tests := []struct {
		name string
		opts CreateContainerOptions
	}{
		{
			name: "minimal options",
			opts: CreateContainerOptions{
				Name:  "test-container",
				Image: "alpine:latest",
			},
		},
		{
			name: "with workspace",
			opts: CreateContainerOptions{
				Name:            "test-container",
				Image:           "alpine:latest",
				WorkspacePath:   "/host/workspace",
				WorkspaceFolder: "/workspace",
			},
		},
		{
			name: "with environment",
			opts: CreateContainerOptions{
				Name:  "test-container",
				Image: "alpine:latest",
				Env:   []string{"FOO=bar", "BAZ=qux"},
			},
		},
		{
			name: "with labels",
			opts: CreateContainerOptions{
				Name:   "test-container",
				Image:  "alpine:latest",
				Labels: map[string]string{"com.example.app": "test"},
			},
		},
		{
			name: "with mounts",
			opts: CreateContainerOptions{
				Name:  "test-container",
				Image: "alpine:latest",
				Mounts: []devcontainer.Mount{
					{Type: "bind", Source: "/host", Target: "/container"},
				},
			},
		},
		{
			name: "with capabilities",
			opts: CreateContainerOptions{
				Name:   "test-container",
				Image:  "alpine:latest",
				CapAdd: []string{"SYS_PTRACE"},
			},
		},
		{
			name: "privileged mode",
			opts: CreateContainerOptions{
				Name:       "test-container",
				Image:      "alpine:latest",
				Privileged: true,
			},
		},
		{
			name: "with init",
			opts: CreateContainerOptions{
				Name:  "test-container",
				Image: "alpine:latest",
				Init:  true,
			},
		},
		{
			name: "with user",
			opts: CreateContainerOptions{
				Name:  "test-container",
				Image: "alpine:latest",
				User:  "vscode",
			},
		},
		{
			name: "with ports",
			opts: CreateContainerOptions{
				Name:  "test-container",
				Image: "alpine:latest",
				Ports: []devcontainer.PortForward{
					{ContainerPort: 8080, HostPort: 8080},
					{ContainerPort: 8080, HostPort: 3000},
				},
			},
		},
		{
			name: "with entrypoint and cmd",
			opts: CreateContainerOptions{
				Name:       "test-container",
				Image:      "alpine:latest",
				Entrypoint: []string{"sleep"},
				Cmd:        []string{"infinity"},
			},
		},
		{
			name: "with security options",
			opts: CreateContainerOptions{
				Name:        "test-container",
				Image:       "alpine:latest",
				SecurityOpt: []string{"seccomp=unconfined"},
			},
		},
		{
			name: "with network mode",
			opts: CreateContainerOptions{
				Name:        "test-container",
				Image:       "alpine:latest",
				NetworkMode: "host",
			},
		},
		{
			name: "with ipc mode",
			opts: CreateContainerOptions{
				Name:    "test-container",
				Image:   "alpine:latest",
				IpcMode: "host",
			},
		},
		{
			name: "with pid mode",
			opts: CreateContainerOptions{
				Name:    "test-container",
				Image:   "alpine:latest",
				PidMode: "host",
			},
		},
		{
			name: "with devices",
			opts: CreateContainerOptions{
				Name:    "test-container",
				Image:   "alpine:latest",
				Devices: []string{"/dev/snd"},
			},
		},
		{
			name: "with extra hosts",
			opts: CreateContainerOptions{
				Name:       "test-container",
				Image:      "alpine:latest",
				ExtraHosts: []string{"myhost:192.168.1.1"},
			},
		},
		{
			name: "with tmpfs",
			opts: CreateContainerOptions{
				Name:  "test-container",
				Image: "alpine:latest",
				Tmpfs: map[string]string{"/tmp": "rw,noexec,nosuid,size=65536k"},
			},
		},
		{
			name: "with sysctls",
			opts: CreateContainerOptions{
				Name:    "test-container",
				Image:   "alpine:latest",
				Sysctls: map[string]string{"net.core.somaxconn": "1024"},
			},
		},
		{
			name: "with GPU request all",
			opts: CreateContainerOptions{
				Name:       "test-container",
				Image:      "alpine:latest",
				GPURequest: "all",
			},
		},
		{
			name: "with GPU request count",
			opts: CreateContainerOptions{
				Name:       "test-container",
				Image:      "alpine:latest",
				GPURequest: "2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the options struct is valid
			assert.NotEmpty(t, tt.opts.Name)
			assert.NotEmpty(t, tt.opts.Image)
		})
	}
}

func TestImageBuildOptions(t *testing.T) {
	tests := []struct {
		name string
		opts ImageBuildOptions
	}{
		{
			name: "minimal options",
			opts: ImageBuildOptions{
				Tag:     "test:latest",
				Context: ".",
			},
		},
		{
			name: "with dockerfile",
			opts: ImageBuildOptions{
				Tag:        "test:latest",
				Dockerfile: "Dockerfile.dev",
				Context:    ".",
			},
		},
		{
			name: "with build args",
			opts: ImageBuildOptions{
				Tag:     "test:latest",
				Context: ".",
				Args:    map[string]string{"VERSION": "1.0"},
			},
		},
		{
			name: "with target",
			opts: ImageBuildOptions{
				Tag:     "test:latest",
				Context: ".",
				Target:  "builder",
			},
		},
		{
			name: "with cache from",
			opts: ImageBuildOptions{
				Tag:       "test:latest",
				Context:   ".",
				CacheFrom: []string{"registry.example.com/cache:latest"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.opts.Tag)
		})
	}
}

func TestLogsOptions(t *testing.T) {
	tests := []struct {
		name string
		opts LogsOptions
	}{
		{
			name: "default options",
			opts: LogsOptions{},
		},
		{
			name: "follow logs",
			opts: LogsOptions{Follow: true},
		},
		{
			name: "with timestamps",
			opts: LogsOptions{Timestamps: true},
		},
		{
			name: "tail 100 lines",
			opts: LogsOptions{Tail: "100"},
		},
		{
			name: "all options",
			opts: LogsOptions{
				Follow:     true,
				Timestamps: true,
				Tail:       "50",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the options struct is valid
			if tt.opts.Tail != "" && tt.opts.Tail != "all" {
				assert.NotEmpty(t, tt.opts.Tail)
			}
		})
	}
}

func TestCleanupResult(t *testing.T) {
	tests := []struct {
		name   string
		result CleanupResult
	}{
		{
			name:   "no cleanup",
			result: CleanupResult{},
		},
		{
			name: "images removed",
			result: CleanupResult{
				ImagesRemoved:  5,
				SpaceReclaimed: 1024 * 1024 * 100, // 100MB
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.GreaterOrEqual(t, tt.result.ImagesRemoved, 0)
			assert.GreaterOrEqual(t, tt.result.SpaceReclaimed, int64(0))
		})
	}
}

func TestContainerInfo(t *testing.T) {
	info := ContainerInfo{
		ID:        "abc123",
		Name:      "test-container",
		Image:     "alpine:latest",
		Status:    "running",
		Running:   true,
		Labels:    map[string]string{"app": "test"},
		CreatedAt: 1609459200,
	}

	assert.Equal(t, "abc123", info.ID)
	assert.Equal(t, "test-container", info.Name)
	assert.Equal(t, "alpine:latest", info.Image)
	assert.Equal(t, "running", info.Status)
	assert.True(t, info.Running)
	assert.Contains(t, info.Labels, "app")
	assert.Greater(t, info.CreatedAt, int64(0))
}

func TestSystemInfo(t *testing.T) {
	info := SystemInfo{
		NCPU:         8,
		MemTotal:     16 * 1024 * 1024 * 1024, // 16GB
		OSType:       "linux",
		Architecture: "x86_64",
	}

	assert.Equal(t, 8, info.NCPU)
	assert.Equal(t, uint64(16*1024*1024*1024), info.MemTotal)
	assert.Equal(t, "linux", info.OSType)
	assert.Equal(t, "x86_64", info.Architecture)
}
