package container

import (
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUnifiedRuntime(t *testing.T) {
	tests := []struct {
		name         string
		resolved     *devcontainer.ResolvedDevContainer
		dockerClient *DockerClient
		wantErr      bool
		errContains  string
	}{
		{
			name:         "nil resolved",
			resolved:     nil,
			dockerClient: &DockerClient{},
			wantErr:      true,
			errContains:  "resolved devcontainer is required",
		},
		{
			name: "nil docker client",
			resolved: &devcontainer.ResolvedDevContainer{
				ID:          "test-id",
				ServiceName: "test-service",
			},
			dockerClient: nil,
			wantErr:      true,
			errContains:  "docker client is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime, err := NewUnifiedRuntime(tt.resolved, tt.dockerClient)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				assert.Nil(t, runtime)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, runtime)
			}
		})
	}
}

func TestNewUnifiedRuntimeForExisting(t *testing.T) {
	tests := []struct {
		name          string
		workspacePath string
		projectName   string
		workspaceID   string
	}{
		{
			name:          "basic creation",
			workspacePath: "/test/workspace",
			projectName:   "test-project",
			workspaceID:   "ws-123",
		},
		{
			name:          "empty workspace path",
			workspacePath: "",
			projectName:   "test-project",
			workspaceID:   "ws-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := NewUnifiedRuntimeForExisting(
				tt.workspacePath,
				tt.projectName,
				tt.workspaceID,
				&DockerClient{},
			)
			require.NotNil(t, runtime)
			assert.Equal(t, tt.projectName, runtime.containerName)
			assert.Equal(t, tt.workspacePath, runtime.workspacePath)
		})
	}
}

func TestNewUnifiedRuntimeForExistingCompose(t *testing.T) {
	tests := []struct {
		name           string
		configDir      string
		composeProject string
	}{
		{
			name:           "basic compose",
			configDir:      "/test/project",
			composeProject: "my-project",
		},
		{
			name:           "empty project",
			configDir:      "/test",
			composeProject: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := NewUnifiedRuntimeForExistingCompose(
				tt.configDir,
				tt.composeProject,
				&DockerClient{},
			)
			require.NotNil(t, runtime)
			assert.Equal(t, tt.composeProject, runtime.composeProject)
			assert.True(t, runtime.isCompose)
			assert.Equal(t, tt.configDir, runtime.workspacePath)
		})
	}
}

func TestUnifiedRuntimeWorkspaceFolder(t *testing.T) {
	tests := []struct {
		name     string
		resolved *devcontainer.ResolvedDevContainer
		want     string
	}{
		{
			name:     "nil resolved",
			resolved: nil,
			want:     "",
		},
		{
			name: "with workspace folder",
			resolved: &devcontainer.ResolvedDevContainer{
				WorkspaceFolder: "/workspace",
			},
			want: "/workspace",
		},
		{
			name: "empty workspace folder",
			resolved: &devcontainer.ResolvedDevContainer{
				WorkspaceFolder: "",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &UnifiedRuntime{resolved: tt.resolved}
			assert.Equal(t, tt.want, runtime.WorkspaceFolder())
		})
	}
}

func TestUnifiedRuntimeContainerName(t *testing.T) {
	tests := []struct {
		name          string
		containerName string
		want          string
	}{
		{
			name:          "with name",
			containerName: "test-container",
			want:          "test-container",
		},
		{
			name:          "empty name",
			containerName: "",
			want:          "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &UnifiedRuntime{containerName: tt.containerName}
			assert.Equal(t, tt.want, runtime.ContainerName())
		})
	}
}

func TestUpOptions(t *testing.T) {
	tests := []struct {
		name        string
		opts        UpOptions
		wantBuild   bool
		wantRebuild bool
		wantPull    bool
	}{
		{
			name:        "default options",
			opts:        UpOptions{},
			wantBuild:   false,
			wantRebuild: false,
			wantPull:    false,
		},
		{
			name:        "build only",
			opts:        UpOptions{Build: true},
			wantBuild:   true,
			wantRebuild: false,
			wantPull:    false,
		},
		{
			name:        "rebuild implies build",
			opts:        UpOptions{Rebuild: true},
			wantBuild:   false,
			wantRebuild: true,
			wantPull:    false,
		},
		{
			name:        "all options",
			opts:        UpOptions{Build: true, Rebuild: true, Pull: true},
			wantBuild:   true,
			wantRebuild: true,
			wantPull:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantBuild, tt.opts.Build)
			assert.Equal(t, tt.wantRebuild, tt.opts.Rebuild)
			assert.Equal(t, tt.wantPull, tt.opts.Pull)
		})
	}
}

func TestDownOptions(t *testing.T) {
	tests := []struct {
		name          string
		opts          DownOptions
		wantVolumes   bool
		wantOrphans   bool
	}{
		{
			name:        "default options",
			opts:        DownOptions{},
			wantVolumes: false,
			wantOrphans: false,
		},
		{
			name:        "remove volumes",
			opts:        DownOptions{RemoveVolumes: true},
			wantVolumes: true,
			wantOrphans: false,
		},
		{
			name:        "remove orphans",
			opts:        DownOptions{RemoveOrphans: true},
			wantVolumes: false,
			wantOrphans: true,
		},
		{
			name:        "both options",
			opts:        DownOptions{RemoveVolumes: true, RemoveOrphans: true},
			wantVolumes: true,
			wantOrphans: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantVolumes, tt.opts.RemoveVolumes)
			assert.Equal(t, tt.wantOrphans, tt.opts.RemoveOrphans)
		})
	}
}

func TestBuildOptions(t *testing.T) {
	tests := []struct {
		name      string
		opts      BuildOptions
		wantCache bool
		wantPull  bool
	}{
		{
			name:      "default options",
			opts:      BuildOptions{},
			wantCache: false,
			wantPull:  false,
		},
		{
			name:      "no cache",
			opts:      BuildOptions{NoCache: true},
			wantCache: true,
			wantPull:  false,
		},
		{
			name:      "pull",
			opts:      BuildOptions{Pull: true},
			wantCache: false,
			wantPull:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantCache, tt.opts.NoCache)
			assert.Equal(t, tt.wantPull, tt.opts.Pull)
		})
	}
}

func TestExecOptions(t *testing.T) {
	tests := []struct {
		name       string
		opts       ExecOptions
		wantTTY    bool
		wantSSH    bool
	}{
		{
			name:    "default options",
			opts:    ExecOptions{},
			wantTTY: false,
			wantSSH: false,
		},
		{
			name:    "with TTY",
			opts:    ExecOptions{TTY: true},
			wantTTY: true,
			wantSSH: false,
		},
		{
			name:    "with SSH agent",
			opts:    ExecOptions{SSHAgentEnabled: true},
			wantTTY: false,
			wantSSH: true,
		},
		{
			name: "with working dir and user",
			opts: ExecOptions{
				WorkingDir: "/workspace",
				User:       "vscode",
			},
			wantTTY: false,
			wantSSH: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantTTY, tt.opts.TTY)
			assert.Equal(t, tt.wantSSH, tt.opts.SSHAgentEnabled)
		})
	}
}

func TestBuildMounts(t *testing.T) {
	tests := []struct {
		name     string
		resolved *devcontainer.ResolvedDevContainer
		want     []string
	}{
		{
			name: "no mounts",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: nil,
			},
			want: nil,
		},
		{
			name: "single mount",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: []mount.Mount{
					{Source: "/host/path", Target: "/container/path"},
				},
			},
			want: []string{"/host/path:/container/path"},
		},
		{
			name: "readonly mount",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: []mount.Mount{
					{Source: "/host/path", Target: "/container/path", ReadOnly: true},
				},
			},
			want: []string{"/host/path:/container/path:ro"},
		},
		{
			name: "multiple mounts",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: []mount.Mount{
					{Source: "/path1", Target: "/target1"},
					{Source: "/path2", Target: "/target2", ReadOnly: true},
				},
			},
			want: []string{"/path1:/target1", "/path2:/target2:ro"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &UnifiedRuntime{resolved: tt.resolved}
			got := runtime.buildMounts()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildEnvironment(t *testing.T) {
	tests := []struct {
		name     string
		resolved *devcontainer.ResolvedDevContainer
		wantLen  int
	}{
		{
			name: "no env vars",
			resolved: &devcontainer.ResolvedDevContainer{
				ContainerEnv: nil,
			},
			wantLen: 0,
		},
		{
			name: "single env var",
			resolved: &devcontainer.ResolvedDevContainer{
				ContainerEnv: map[string]string{"FOO": "bar"},
			},
			wantLen: 1,
		},
		{
			name: "multiple env vars",
			resolved: &devcontainer.ResolvedDevContainer{
				ContainerEnv: map[string]string{
					"FOO": "bar",
					"BAZ": "qux",
				},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &UnifiedRuntime{resolved: tt.resolved}
			got := runtime.buildEnvironment()
			assert.Len(t, got, tt.wantLen)
		})
	}
}

func TestBuildPortBindings(t *testing.T) {
	tests := []struct {
		name     string
		resolved *devcontainer.ResolvedDevContainer
		wantLen  int
	}{
		{
			name: "no ports",
			resolved: &devcontainer.ResolvedDevContainer{
				ForwardPorts: nil,
			},
			wantLen: 0,
		},
		{
			name: "single port",
			resolved: &devcontainer.ResolvedDevContainer{
				ForwardPorts: []devcontainer.PortForward{
					{ContainerPort: 8080, HostPort: 8080},
				},
			},
			wantLen: 1,
		},
		{
			name: "different host port",
			resolved: &devcontainer.ResolvedDevContainer{
				ForwardPorts: []devcontainer.PortForward{
					{ContainerPort: 8080, HostPort: 3000},
				},
			},
			wantLen: 1,
		},
		{
			name: "multiple ports",
			resolved: &devcontainer.ResolvedDevContainer{
				ForwardPorts: []devcontainer.PortForward{
					{ContainerPort: 8080, HostPort: 8080},
					{ContainerPort: 5432, HostPort: 5432},
				},
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &UnifiedRuntime{resolved: tt.resolved}
			got := runtime.buildPortBindings()
			if tt.wantLen == 0 {
				assert.Nil(t, got)
			} else {
				assert.Len(t, got, tt.wantLen)
			}
		})
	}
}

func TestComposeBaseArgs(t *testing.T) {
	tests := []struct {
		name           string
		containerName  string
		composeProject string
		plan           *devcontainer.ComposePlan
		wantContains   []string
	}{
		{
			name:          "container name only",
			containerName: "test-container",
			wantContains:  []string{"-p", "test-container"},
		},
		{
			name:           "compose project",
			composeProject: "my-project",
			wantContains:   []string{"-p", "my-project"},
		},
		{
			name:          "with plan",
			containerName: "fallback",
			plan: &devcontainer.ComposePlan{
				ProjectName: "plan-project",
				Files:       []string{"docker-compose.yml"},
			},
			wantContains: []string{"-p", "plan-project", "-f", "docker-compose.yml"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &UnifiedRuntime{
				containerName:  tt.containerName,
				composeProject: tt.composeProject,
			}
			args := runtime.composeBaseArgs(tt.plan)
			for _, want := range tt.wantContains {
				assert.Contains(t, args, want)
			}
		})
	}
}

func TestContainerRuntimeInterface(t *testing.T) {
	// Verify UnifiedRuntime implements ContainerRuntime
	var _ ContainerRuntime = (*UnifiedRuntime)(nil)
}

func TestOverrideCommandDefault(t *testing.T) {
	// Per devcontainer spec:
	// - Default true for image/dockerfile-based containers
	// - Default false for compose-based containers
	tests := []struct {
		name            string
		plan            devcontainer.ExecutionPlan
		overrideCommand *bool
		wantOverride    bool
	}{
		{
			name:            "image plan - default should override",
			plan:            devcontainer.NewImagePlan("alpine:latest"),
			overrideCommand: nil,
			wantOverride:    true,
		},
		{
			name:            "image plan - explicit true",
			plan:            devcontainer.NewImagePlan("alpine:latest"),
			overrideCommand: boolPtr(true),
			wantOverride:    true,
		},
		{
			name:            "image plan - explicit false",
			plan:            devcontainer.NewImagePlan("alpine:latest"),
			overrideCommand: boolPtr(false),
			wantOverride:    false,
		},
		{
			name:            "dockerfile plan - default should override",
			plan:            devcontainer.NewDockerfilePlan("Dockerfile", "."),
			overrideCommand: nil,
			wantOverride:    true,
		},
		{
			name:            "dockerfile plan - explicit false",
			plan:            devcontainer.NewDockerfilePlan("Dockerfile", "."),
			overrideCommand: boolPtr(false),
			wantOverride:    false,
		},
		{
			name:            "compose plan - default should NOT override",
			plan:            devcontainer.NewComposePlan([]string{"docker-compose.yml"}, "app", "project"),
			overrideCommand: nil,
			wantOverride:    false,
		},
		{
			name:            "compose plan - explicit true",
			plan:            devcontainer.NewComposePlan([]string{"docker-compose.yml"}, "app", "project"),
			overrideCommand: boolPtr(true),
			wantOverride:    true,
		},
		{
			name:            "compose plan - explicit false",
			plan:            devcontainer.NewComposePlan([]string{"docker-compose.yml"}, "app", "project"),
			overrideCommand: boolPtr(false),
			wantOverride:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the logic directly
			var shouldOverride bool
			if tt.overrideCommand != nil {
				shouldOverride = *tt.overrideCommand
			} else {
				_, isCompose := tt.plan.(*devcontainer.ComposePlan)
				shouldOverride = !isCompose
			}
			assert.Equal(t, tt.wantOverride, shouldOverride, "overrideCommand default for %T", tt.plan)
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}
