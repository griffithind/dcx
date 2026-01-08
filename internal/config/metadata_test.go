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

func TestDeepMergeVSCodeCustomizations(t *testing.T) {
	t.Run("extensions are unioned", func(t *testing.T) {
		local := &DevcontainerConfig{
			Customizations: map[string]interface{}{
				"vscode": map[string]interface{}{
					"extensions": []interface{}{"local.ext1", "shared.ext"},
				},
			},
		}
		image := []DevcontainerConfig{{
			Customizations: map[string]interface{}{
				"vscode": map[string]interface{}{
					"extensions": []interface{}{"image.ext1", "shared.ext"},
				},
			},
		}}
		result := MergeMetadata(local, image)
		vscode := result.Customizations["vscode"].(map[string]interface{})
		extensions := vscode["extensions"].([]interface{})
		// Should have: local.ext1, shared.ext, image.ext1 (no duplicate shared.ext)
		assert.Len(t, extensions, 3)
		assert.Contains(t, extensions, "local.ext1")
		assert.Contains(t, extensions, "shared.ext")
		assert.Contains(t, extensions, "image.ext1")
	})

	t.Run("settings are merged with local taking precedence", func(t *testing.T) {
		local := &DevcontainerConfig{
			Customizations: map[string]interface{}{
				"vscode": map[string]interface{}{
					"settings": map[string]interface{}{
						"editor.fontSize":   14,
						"editor.tabSize":    4,
						"local.setting":     "localValue",
					},
				},
			},
		}
		image := []DevcontainerConfig{{
			Customizations: map[string]interface{}{
				"vscode": map[string]interface{}{
					"settings": map[string]interface{}{
						"editor.fontSize": 12,        // local wins
						"image.setting":   "imageValue",
					},
				},
			},
		}}
		result := MergeMetadata(local, image)
		vscode := result.Customizations["vscode"].(map[string]interface{})
		settings := vscode["settings"].(map[string]interface{})
		assert.Equal(t, 14, settings["editor.fontSize"])         // local wins
		assert.Equal(t, 4, settings["editor.tabSize"])           // local only
		assert.Equal(t, "localValue", settings["local.setting"]) // local only
		assert.Equal(t, "imageValue", settings["image.setting"]) // image only
	})

	t.Run("both extensions and settings are merged together", func(t *testing.T) {
		local := &DevcontainerConfig{
			Customizations: map[string]interface{}{
				"vscode": map[string]interface{}{
					"extensions": []interface{}{"local.ext"},
					"settings": map[string]interface{}{
						"editor.fontSize": 14,
					},
				},
			},
		}
		image := []DevcontainerConfig{{
			Customizations: map[string]interface{}{
				"vscode": map[string]interface{}{
					"extensions": []interface{}{"image.ext"},
					"settings": map[string]interface{}{
						"editor.fontSize": 12,
						"image.setting":   "value",
					},
				},
			},
		}}
		result := MergeMetadata(local, image)
		vscode := result.Customizations["vscode"].(map[string]interface{})
		extensions := vscode["extensions"].([]interface{})
		settings := vscode["settings"].(map[string]interface{})
		assert.Len(t, extensions, 2)
		assert.Equal(t, 14, settings["editor.fontSize"]) // local wins
		assert.Equal(t, "value", settings["image.setting"])
	})

	t.Run("non-vscode customizations use simple merge", func(t *testing.T) {
		local := &DevcontainerConfig{
			Customizations: map[string]interface{}{
				"jetbrains": map[string]interface{}{
					"localSetting": "value",
				},
			},
		}
		image := []DevcontainerConfig{{
			Customizations: map[string]interface{}{
				"jetbrains": map[string]interface{}{
					"imageSetting": "value",
				},
			},
		}}
		result := MergeMetadata(local, image)
		jetbrains := result.Customizations["jetbrains"].(map[string]interface{})
		// Local customization wins entirely (not deep merged)
		assert.Equal(t, "value", jetbrains["localSetting"])
		assert.NotContains(t, jetbrains, "imageSetting")
	})
}
