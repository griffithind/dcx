package container

import (
	"testing"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/report"
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
				Identity: devcontainer.Identity{WorkspaceID: "test-id", ResourceName: "test-service"},
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
			wantTmpfs:  map[string]string{"/run/secrets": "rw,noexec,nosuid,size=16m"},
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
			wantTmpfs: map[string]string{"/run/secrets": "rw,noexec,nosuid,size=16m"},
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
			wantTmpfs: map[string]string{"/run/secrets": "rw,noexec,nosuid,size=16m"},
		},
		{
			name: "tmpfs mount",
			resolved: &devcontainer.ResolvedDevContainer{
				Mounts: []devcontainer.Mount{
					{Target: "/tmp/test", Type: "tmpfs"},
				},
			},
			wantMounts: nil,
			wantTmpfs:  map[string]string{"/tmp/test": "", "/run/secrets": "rw,noexec,nosuid,size=16m"},
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
			wantTmpfs: map[string]string{"/run": "", "/run/secrets": "rw,noexec,nosuid,size=16m"},
		},
		{
			name: "with runtime secrets keeps /run/secrets tmpfs",
			resolved: &devcontainer.ResolvedDevContainer{
				RuntimeSecrets: map[string]devcontainer.SecretConfig{
					"MY_SECRET": "echo secret-value",
				},
			},
			wantMounts: nil,
			wantTmpfs:  map[string]string{"/run/secrets": "rw,noexec,nosuid,size=16m"},
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
			runtime := &Provisioner{resolved: tt.resolved, ssh: &sshEndpoint{hostPort: 40001, publish: true}}
			got := runtime.buildPortBindings()
			assert.Len(t, got, tt.wantLen)

			// The last entry must be the dcx SSH listener.
			ssh := got[len(got)-1]
			assert.Equal(t, agentproto.ListenPort, ssh.ContainerPort)
			assert.Equal(t, "127.0.0.1", ssh.Host)
			assert.Equal(t, 40001, ssh.HostPort)
			assert.False(t, ssh.EphemeralHostPort)
		})
	}
}

// Feature permissions are merged once, by the builder; the spec renders
// them as resolved (Q1: they used to be appended a second time).
func TestFeaturePermissionsAreNotDuplicated(t *testing.T) {
	feats := []*features.Feature{
		{ID: "dind", Metadata: &features.FeatureMetadata{ID: "dind", Name: "Docker in Docker", Privileged: true, CapAdd: []string{"NET_ADMIN"}}},
		{ID: "go", Metadata: &features.FeatureMetadata{ID: "go", Name: "Go", CapAdd: []string{"SYS_PTRACE"}}},
		{ID: "node", Metadata: &features.FeatureMetadata{ID: "node", Name: "Node"}},
	}
	r := &Provisioner{report: report.Discard, resolved: &devcontainer.ResolvedDevContainer{
		Plan:       devcontainer.NewImagePlan("img"),
		Features:   feats,
		CapAdd:     []string{"NET_ADMIN", "SYS_PTRACE"},
		Privileged: true,
	}}
	s := r.spec("img")
	assert.Equal(t, []string{"NET_ADMIN", "SYS_PTRACE"}, s.CapAdd)
	assert.True(t, s.Privileged)
	assert.Equal(t, []string{"Docker in Docker (privileged)", "Go (capabilities)"}, features.ElevatedFeatures(feats))
}
