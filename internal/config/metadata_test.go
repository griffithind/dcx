package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseImageMetadata(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []DevcontainerConfig
		wantErr  bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:  "single config",
			input: `[{"remoteUser": "vscode", "workspaceFolder": "/workspace"}]`,
			expected: []DevcontainerConfig{
				{RemoteUser: "vscode", WorkspaceFolder: "/workspace"},
			},
		},
		{
			name:  "multiple configs",
			input: `[{"remoteUser": "node"}, {"remoteUser": "vscode", "capAdd": ["SYS_PTRACE"]}]`,
			expected: []DevcontainerConfig{
				{RemoteUser: "node"},
				{RemoteUser: "vscode", CapAdd: []string{"SYS_PTRACE"}},
			},
		},
		{
			name:    "invalid JSON",
			input:   `not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseImageMetadata(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMergeMetadata(t *testing.T) {
	t.Run("empty image configs returns local", func(t *testing.T) {
		local := &DevcontainerConfig{RemoteUser: "local"}
		result := MergeMetadata(local, nil)
		assert.Equal(t, "local", result.RemoteUser)
	})

	t.Run("local single values take precedence", func(t *testing.T) {
		local := &DevcontainerConfig{RemoteUser: "local", WorkspaceFolder: "/local"}
		image := []DevcontainerConfig{{RemoteUser: "image", WorkspaceFolder: "/image"}}
		result := MergeMetadata(local, image)
		assert.Equal(t, "local", result.RemoteUser)
		assert.Equal(t, "/local", result.WorkspaceFolder)
	})

	t.Run("image fills empty local values", func(t *testing.T) {
		local := &DevcontainerConfig{RemoteUser: "local"}
		image := []DevcontainerConfig{{WorkspaceFolder: "/image", UserEnvProbe: "loginShell"}}
		result := MergeMetadata(local, image)
		assert.Equal(t, "local", result.RemoteUser)
		assert.Equal(t, "/image", result.WorkspaceFolder)
		assert.Equal(t, "loginShell", result.UserEnvProbe)
	})

	t.Run("boolean true wins", func(t *testing.T) {
		val := true
		local := &DevcontainerConfig{}
		image := []DevcontainerConfig{{Init: &val, Privileged: &val}}
		result := MergeMetadata(local, image)
		require.NotNil(t, result.Init)
		assert.True(t, *result.Init)
		require.NotNil(t, result.Privileged)
		assert.True(t, *result.Privileged)
	})

	t.Run("local boolean false overrides image true", func(t *testing.T) {
		localVal := false
		imageVal := true
		local := &DevcontainerConfig{Init: &localVal}
		image := []DevcontainerConfig{{Init: &imageVal}}
		result := MergeMetadata(local, image)
		require.NotNil(t, result.Init)
		assert.False(t, *result.Init)
	})

	t.Run("arrays are unioned", func(t *testing.T) {
		local := &DevcontainerConfig{
			CapAdd:      []string{"SYS_PTRACE"},
			SecurityOpt: []string{"seccomp=unconfined"},
		}
		image := []DevcontainerConfig{{
			CapAdd:      []string{"NET_ADMIN", "SYS_PTRACE"}, // SYS_PTRACE is duplicate
			SecurityOpt: []string{"apparmor=unconfined"},
		}}
		result := MergeMetadata(local, image)
		assert.ElementsMatch(t, []string{"SYS_PTRACE", "NET_ADMIN"}, result.CapAdd)
		assert.ElementsMatch(t, []string{"seccomp=unconfined", "apparmor=unconfined"}, result.SecurityOpt)
	})

	t.Run("environment maps are merged", func(t *testing.T) {
		local := &DevcontainerConfig{
			ContainerEnv: map[string]string{"LOCAL": "value", "SHARED": "local"},
		}
		image := []DevcontainerConfig{{
			ContainerEnv: map[string]string{"IMAGE": "value", "SHARED": "image"},
		}}
		result := MergeMetadata(local, image)
		assert.Equal(t, "value", result.ContainerEnv["LOCAL"])
		assert.Equal(t, "value", result.ContainerEnv["IMAGE"])
		assert.Equal(t, "local", result.ContainerEnv["SHARED"]) // local wins
	})

	t.Run("features are merged", func(t *testing.T) {
		local := &DevcontainerConfig{
			Features: map[string]interface{}{
				"ghcr.io/devcontainers/features/git:1": map[string]interface{}{},
			},
		}
		image := []DevcontainerConfig{{
			Features: map[string]interface{}{
				"ghcr.io/devcontainers/features/node:1":   map[string]interface{}{},
				"ghcr.io/devcontainers/features/python:1": map[string]interface{}{},
			},
		}}
		result := MergeMetadata(local, image)
		assert.Len(t, result.Features, 3)
		assert.Contains(t, result.Features, "ghcr.io/devcontainers/features/git:1")
		assert.Contains(t, result.Features, "ghcr.io/devcontainers/features/node:1")
		assert.Contains(t, result.Features, "ghcr.io/devcontainers/features/python:1")
	})

	t.Run("multiple image configs merge in order", func(t *testing.T) {
		local := &DevcontainerConfig{RemoteUser: "local"}
		image := []DevcontainerConfig{
			{WorkspaceFolder: "/first", UserEnvProbe: "none"},
			{WorkspaceFolder: "/second"}, // overwrites /first since local is empty
		}
		result := MergeMetadata(local, image)
		assert.Equal(t, "local", result.RemoteUser)
		// First wins for workspaceFolder since target already set
		assert.Equal(t, "/first", result.WorkspaceFolder)
		assert.Equal(t, "none", result.UserEnvProbe)
	})

	t.Run("forward ports are unioned", func(t *testing.T) {
		local := &DevcontainerConfig{
			ForwardPorts: []interface{}{3000, "8080:80"},
		}
		image := []DevcontainerConfig{{
			ForwardPorts: []interface{}{float64(3000), 5000}, // float64 from JSON, 3000 is duplicate
		}}
		result := MergeMetadata(local, image)
		// Should have 3000, 8080:80, 5000 (no duplicate 3000)
		assert.Len(t, result.ForwardPorts, 3)
	})

	t.Run("mounts are unioned by target", func(t *testing.T) {
		local := &DevcontainerConfig{
			Mounts: []Mount{
				{Source: "/local", Target: "/mount1", Type: "bind"},
			},
		}
		image := []DevcontainerConfig{{
			Mounts: []Mount{
				{Source: "/image", Target: "/mount1", Type: "bind"}, // duplicate target
				{Source: "/image2", Target: "/mount2", Type: "bind"},
			},
		}}
		result := MergeMetadata(local, image)
		assert.Len(t, result.Mounts, 2)
		// Local mount1 wins
		assert.Equal(t, "/local", result.Mounts[0].Source)
		assert.Equal(t, "/mount1", result.Mounts[0].Target)
	})
}

func TestUnionStrings(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected []string
	}{
		{
			name:     "both empty",
			a:        nil,
			b:        nil,
			expected: nil,
		},
		{
			name:     "a empty",
			a:        nil,
			b:        []string{"one", "two"},
			expected: []string{"one", "two"},
		},
		{
			name:     "b empty",
			a:        []string{"one", "two"},
			b:        nil,
			expected: []string{"one", "two"},
		},
		{
			name:     "no overlap",
			a:        []string{"one", "two"},
			b:        []string{"three", "four"},
			expected: []string{"one", "two", "three", "four"},
		},
		{
			name:     "with duplicates",
			a:        []string{"one", "two"},
			b:        []string{"two", "three"},
			expected: []string{"one", "two", "three"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := unionStrings(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}
