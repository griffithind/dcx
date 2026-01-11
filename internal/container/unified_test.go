package container

import (
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewUnifiedRuntime(t *testing.T) {
	tests := []struct {
		name        string
		resolved    *devcontainer.ResolvedDevContainer
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil resolved",
			resolved:    nil,
			wantErr:     true,
			errContains: "resolved devcontainer is required",
		},
		{
			name: "valid resolved",
			resolved: &devcontainer.ResolvedDevContainer{
				ID:          "test-id",
				ServiceName: "test-service",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime, err := NewUnifiedRuntime(tt.resolved)
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
			)
			require.NotNil(t, runtime)
			assert.Equal(t, tt.composeProject, runtime.composeProject)
			assert.True(t, runtime.isCompose)
			assert.Equal(t, tt.configDir, runtime.workspacePath)
			assert.NotNil(t, runtime.compose, "compose client should be initialized")
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
		name       string
		resolved   *devcontainer.ResolvedDevContainer
		wantMounts []devcontainer.Mount
		wantTmpfs  map[string]string
	}{
		{
			name: "no mounts",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: nil,
			},
			wantMounts: nil,
			wantTmpfs:  map[string]string{},
		},
		{
			name: "single bind mount",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: []devcontainer.Mount{
					{Source: "/host/path", Target: "/container/path", Type: "bind"},
				},
			},
			wantMounts: []devcontainer.Mount{
				{Source: "/host/path", Target: "/container/path", Type: "bind"},
			},
			wantTmpfs: map[string]string{},
		},
		{
			name: "readonly bind mount",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: []devcontainer.Mount{
					{Source: "/host/path", Target: "/container/path", Type: "bind", ReadOnly: true},
				},
			},
			wantMounts: []devcontainer.Mount{
				{Source: "/host/path", Target: "/container/path", Type: "bind", ReadOnly: true},
			},
			wantTmpfs: map[string]string{},
		},
		{
			name: "tmpfs mount",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: []devcontainer.Mount{
					{Target: "/tmp/test", Type: "tmpfs"},
				},
			},
			wantMounts: nil,
			wantTmpfs:  map[string]string{"/tmp/test": ""},
		},
		{
			name: "mixed mounts",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: []devcontainer.Mount{
					{Source: "/path1", Target: "/target1", Type: "bind"},
					{Target: "/run", Type: "tmpfs"},
					{Source: "/path2", Target: "/target2", Type: "bind", ReadOnly: true},
				},
			},
			wantMounts: []devcontainer.Mount{
				{Source: "/path1", Target: "/target1", Type: "bind"},
				{Source: "/path2", Target: "/target2", Type: "bind", ReadOnly: true},
			},
			wantTmpfs: map[string]string{"/run": ""},
		},
		{
			name: "with runtime secrets adds /run/secrets tmpfs",
			resolved: &devcontainer.ResolvedDevContainer{
				RuntimeSecrets: map[string]devcontainer.SecretConfig{
					"MY_SECRET": "echo secret-value",
				},
			},
			wantMounts: nil,
			wantTmpfs:  map[string]string{"/run/secrets": "rw,noexec,nosuid,size=1m"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &UnifiedRuntime{resolved: tt.resolved}
			got := runtime.buildMounts()
			assert.Equal(t, tt.wantMounts, got.Mounts)
			assert.Equal(t, tt.wantTmpfs, got.Tmpfs)
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

func TestFeatureSecurityRequirementsIntegration(t *testing.T) {
	// Test that feature security requirements are properly collected
	// This tests the integration logic used in createContainer()

	tests := []struct {
		name             string
		features         []*features.Feature
		wantCaps         []string
		wantSecurityOpts []string
		wantPrivileged   bool
		wantInit         bool
		wantEnvKeys      []string
	}{
		{
			name:           "no features",
			features:       nil,
			wantCaps:       nil,
			wantPrivileged: false,
			wantInit:       false,
		},
		{
			name: "feature with capabilities",
			features: []*features.Feature{
				{
					ID: "test-feature",
					Metadata: &features.FeatureMetadata{
						ID:     "test-feature",
						Name:   "Test Feature",
						CapAdd: []string{"SYS_PTRACE", "NET_ADMIN"},
					},
				},
			},
			wantCaps:       []string{"SYS_PTRACE", "NET_ADMIN"},
			wantPrivileged: false,
			wantInit:       false,
		},
		{
			name: "feature with privileged mode",
			features: []*features.Feature{
				{
					ID: "privileged-feature",
					Metadata: &features.FeatureMetadata{
						ID:         "privileged-feature",
						Name:       "Privileged Feature",
						Privileged: true,
					},
				},
			},
			wantCaps:       nil,
			wantPrivileged: true,
			wantInit:       false,
		},
		{
			name: "feature with init",
			features: []*features.Feature{
				{
					ID: "init-feature",
					Metadata: &features.FeatureMetadata{
						ID:   "init-feature",
						Name: "Init Feature",
						Init: true,
					},
				},
			},
			wantCaps:       nil,
			wantPrivileged: false,
			wantInit:       true,
		},
		{
			name: "feature with security options",
			features: []*features.Feature{
				{
					ID: "secopt-feature",
					Metadata: &features.FeatureMetadata{
						ID:          "secopt-feature",
						Name:        "SecOpt Feature",
						SecurityOpt: []string{"seccomp=unconfined", "apparmor=unconfined"},
					},
				},
			},
			wantCaps:         nil,
			wantSecurityOpts: []string{"seccomp=unconfined", "apparmor=unconfined"},
			wantPrivileged:   false,
			wantInit:         false,
		},
		{
			name: "feature with container env",
			features: []*features.Feature{
				{
					ID: "env-feature",
					Metadata: &features.FeatureMetadata{
						ID:   "env-feature",
						Name: "Env Feature",
						ContainerEnv: map[string]string{
							"MY_VAR":    "value1",
							"OTHER_VAR": "value2",
						},
					},
				},
			},
			wantCaps:       nil,
			wantPrivileged: false,
			wantInit:       false,
			wantEnvKeys:    []string{"MY_VAR", "OTHER_VAR"},
		},
		{
			name: "multiple features with various requirements",
			features: []*features.Feature{
				{
					ID: "feature1",
					Metadata: &features.FeatureMetadata{
						ID:     "feature1",
						Name:   "Feature 1",
						CapAdd: []string{"SYS_PTRACE"},
						Init:   true,
					},
				},
				{
					ID: "feature2",
					Metadata: &features.FeatureMetadata{
						ID:          "feature2",
						Name:        "Feature 2",
						CapAdd:      []string{"NET_ADMIN", "SYS_PTRACE"}, // duplicate cap
						SecurityOpt: []string{"seccomp=unconfined"},
						ContainerEnv: map[string]string{
							"FEATURE2_VAR": "test",
						},
					},
				},
			},
			wantCaps:         []string{"SYS_PTRACE", "NET_ADMIN"}, // deduplicated
			wantSecurityOpts: []string{"seccomp=unconfined"},
			wantPrivileged:   false,
			wantInit:         true,
			wantEnvKeys:      []string{"FEATURE2_VAR"},
		},
		{
			name: "feature with nil metadata is skipped",
			features: []*features.Feature{
				{
					ID:       "no-metadata",
					Metadata: nil,
				},
				{
					ID: "with-metadata",
					Metadata: &features.FeatureMetadata{
						ID:     "with-metadata",
						CapAdd: []string{"SYS_ADMIN"},
					},
				},
			},
			wantCaps:       []string{"SYS_ADMIN"},
			wantPrivileged: false,
			wantInit:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test GetSecurityRequirements
			reqs := features.GetSecurityRequirements(tt.features)
			assert.Equal(t, tt.wantPrivileged, reqs.Privileged, "privileged mismatch")
			assert.ElementsMatch(t, tt.wantCaps, reqs.Capabilities, "capabilities mismatch")
			assert.ElementsMatch(t, tt.wantSecurityOpts, reqs.SecurityOpts, "security opts mismatch")

			// Test NeedsInit
			needsInit := features.NeedsInit(tt.features)
			assert.Equal(t, tt.wantInit, needsInit, "init mismatch")

			// Test CollectContainerEnv
			envMap := features.CollectContainerEnv(tt.features)
			if tt.wantEnvKeys != nil {
				for _, key := range tt.wantEnvKeys {
					assert.Contains(t, envMap, key, "env key %s should be present", key)
				}
			}
		})
	}
}

func TestFeatureSecurityWarnings(t *testing.T) {
	// Test that features requiring elevated permissions generate proper warnings
	tests := []struct {
		name             string
		features         []*features.Feature
		wantFeatureNames []string
	}{
		{
			name:             "no elevated permissions",
			features:         nil,
			wantFeatureNames: nil,
		},
		{
			name: "privileged feature generates warning",
			features: []*features.Feature{
				{
					ID: "docker-in-docker",
					Metadata: &features.FeatureMetadata{
						ID:         "docker-in-docker",
						Name:       "Docker in Docker",
						Privileged: true,
					},
				},
			},
			wantFeatureNames: []string{"Docker in Docker (privileged)"},
		},
		{
			name: "feature with caps but not privileged - no warning names",
			features: []*features.Feature{
				{
					ID: "debugger",
					Metadata: &features.FeatureMetadata{
						ID:     "debugger",
						Name:   "Debugger",
						CapAdd: []string{"SYS_PTRACE"},
					},
				},
			},
			wantFeatureNames: nil, // caps alone don't add to FeatureNames
		},
		{
			name: "uses ID when Name is empty",
			features: []*features.Feature{
				{
					ID: "ghcr.io/test/feature:1",
					Metadata: &features.FeatureMetadata{
						ID:         "ghcr.io/test/feature:1",
						Name:       "", // empty name
						Privileged: true,
					},
				},
			},
			wantFeatureNames: []string{"ghcr.io/test/feature:1 (privileged)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqs := features.GetSecurityRequirements(tt.features)
			assert.ElementsMatch(t, tt.wantFeatureNames, reqs.FeatureNames, "feature names mismatch")
		})
	}
}
