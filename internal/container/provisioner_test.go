package container

import (
	"github.com/griffithind/dcx/internal/agentproto"
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProvisioner(t *testing.T) {
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
			runtime, err := NewProvisioner(NewDockerCLI(), nil, tt.resolved)
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
			wantTmpfs:  map[string]string{"/run/secrets": "rw,noexec,nosuid,size=1m"},
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
			wantTmpfs: map[string]string{"/run/secrets": "rw,noexec,nosuid,size=1m"},
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
			wantTmpfs: map[string]string{"/run/secrets": "rw,noexec,nosuid,size=1m"},
		},
		{
			name: "tmpfs mount",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: []devcontainer.Mount{
					{Target: "/tmp/test", Type: "tmpfs"},
				},
			},
			wantMounts: nil,
			wantTmpfs:  map[string]string{"/tmp/test": "", "/run/secrets": "rw,noexec,nosuid,size=1m"},
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
			wantTmpfs: map[string]string{"/run": "", "/run/secrets": "rw,noexec,nosuid,size=1m"},
		},
		{
			name: "with runtime secrets keeps /run/secrets tmpfs",
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
			runtime := &Provisioner{resolved: tt.resolved}
			mounts, tmpfs := runtime.buildMounts()
			assert.Equal(t, tt.wantMounts, mounts)
			assert.Equal(t, tt.wantTmpfs, tmpfs)
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
			runtime := &Provisioner{resolved: tt.resolved}
			got := runtime.buildEnvironment()
			assert.Len(t, got, tt.wantLen)
		})
	}
}

func TestBuildPortBindings(t *testing.T) {
	// buildPortBindings always appends the dcx SSH listener in addition to
	// any user-configured forwardPorts. Expected lengths account for that
	// extra entry.
	tests := []struct {
		name     string
		resolved *devcontainer.ResolvedDevContainer
		wantLen  int // includes the trailing dcx SSH port
	}{
		{
			name: "no user ports (SSH only)",
			resolved: &devcontainer.ResolvedDevContainer{
				ForwardPorts: nil,
			},
			wantLen: 1,
		},
		{
			name: "single port",
			resolved: &devcontainer.ResolvedDevContainer{
				ForwardPorts: []devcontainer.PortForward{
					{ContainerPort: 8080, HostPort: 8080},
				},
			},
			wantLen: 2,
		},
		{
			name: "different host port",
			resolved: &devcontainer.ResolvedDevContainer{
				ForwardPorts: []devcontainer.PortForward{
					{ContainerPort: 8080, HostPort: 3000},
				},
			},
			wantLen: 2,
		},
		{
			name: "multiple ports",
			resolved: &devcontainer.ResolvedDevContainer{
				ForwardPorts: []devcontainer.PortForward{
					{ContainerPort: 8080, HostPort: 8080},
					{ContainerPort: 5432, HostPort: 5432},
				},
			},
			wantLen: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtime := &Provisioner{resolved: tt.resolved}
			got := runtime.buildPortBindings()
			assert.Len(t, got, tt.wantLen)

			// The last entry must be the dcx SSH listener.
			ssh := got[len(got)-1]
			assert.Equal(t, agentproto.ListenPort, ssh.ContainerPort)
			assert.Equal(t, "127.0.0.1", ssh.Host)
			assert.True(t, ssh.EphemeralHostPort)
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
			runtime := &Provisioner{resolved: &devcontainer.ResolvedDevContainer{
				Plan:      tt.plan,
				RawConfig: &devcontainer.DevContainerConfig{OverrideCommand: tt.overrideCommand},
			}}
			assert.Equal(t, tt.wantOverride, runtime.overrideCommand(), "overrideCommand default for %T", tt.plan)
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
