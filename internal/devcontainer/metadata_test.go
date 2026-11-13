package devcontainer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseImageMetadata(t *testing.T) {
	tests := []struct {
		name       string
		labelValue string
		wantCount  int
		wantErr    bool
		check      func(*testing.T, []DevContainerConfig)
	}{
		{
			name:       "empty label",
			labelValue: "",
			wantCount:  0,
		},
		{
			name:       "single config",
			labelValue: `[{"remoteUser": "vscode"}]`,
			wantCount:  1,
			check: func(t *testing.T, configs []DevContainerConfig) {
				assert.Equal(t, "vscode", configs[0].RemoteUser)
			},
		},
		{
			name:       "multiple configs",
			labelValue: `[{"remoteUser": "vscode"}, {"containerEnv": {"FOO": "bar"}}]`,
			wantCount:  2,
			check: func(t *testing.T, configs []DevContainerConfig) {
				assert.Equal(t, "vscode", configs[0].RemoteUser)
				assert.Equal(t, map[string]string{"FOO": "bar"}, configs[1].ContainerEnv)
			},
		},
		{
			name:       "config with features",
			labelValue: `[{"features": {"ghcr.io/devcontainers/features/docker-in-docker:2": {}}}]`,
			wantCount:  1,
			check: func(t *testing.T, configs []DevContainerConfig) {
				require.NotNil(t, configs[0].Features)
				_, hasDocker := configs[0].Features["ghcr.io/devcontainers/features/docker-in-docker:2"]
				assert.True(t, hasDocker)
			},
		},
		{
			name:       "invalid JSON",
			labelValue: `{not valid}`,
			wantErr:    true,
		},
		{
			// A single object is accepted, as by the reference implementation (S9).
			name:       "single object",
			labelValue: `{"remoteUser": "vscode"}`,
			wantCount:  1,
			check: func(t *testing.T, configs []DevContainerConfig) {
				assert.Equal(t, "vscode", configs[0].RemoteUser)
			},
		},
		{
			name:       "feature entry with id and entrypoint",
			labelValue: `[{"id": "ghcr.io/devcontainers/features/docker-in-docker:2", "entrypoint": "/usr/local/share/docker-init.sh"}]`,
			wantCount:  1,
			check: func(t *testing.T, configs []DevContainerConfig) {
				assert.Equal(t, "ghcr.io/devcontainers/features/docker-in-docker:2", configs[0].ID)
				assert.Equal(t, "/usr/local/share/docker-init.sh", configs[0].Entrypoint)
			},
		},
		{
			name:       "neither array nor object",
			labelValue: `42`,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs, err := ParseImageMetadata(tt.labelValue)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, configs, tt.wantCount)
			if tt.check != nil && len(configs) > 0 {
				tt.check(t, configs)
			}
		})
	}
}

func TestMergeMetadata(t *testing.T) {
	tests := []struct {
		name         string
		local        *DevContainerConfig
		imageConfigs []DevContainerConfig
		check        func(*testing.T, *DevContainerConfig)
	}{
		{
			name: "empty image configs returns local",
			local: &DevContainerConfig{
				Image:      "alpine",
				RemoteUser: "vscode",
			},
			imageConfigs: nil,
			check: func(t *testing.T, merged *DevContainerConfig) {
				assert.Equal(t, "alpine", merged.Image)
				assert.Equal(t, "vscode", merged.RemoteUser)
			},
		},
		{
			name: "local takes precedence for single values",
			local: &DevContainerConfig{
				Image:      "local-image",
				RemoteUser: "local-user",
			},
			imageConfigs: []DevContainerConfig{
				{
					Image:      "image-from-metadata",
					RemoteUser: "image-user",
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				assert.Equal(t, "local-image", merged.Image)
				assert.Equal(t, "local-user", merged.RemoteUser)
			},
		},
		{
			name: "image values fill in gaps in local config",
			local: &DevContainerConfig{
				Image: "local-image",
				// RemoteUser not set
			},
			imageConfigs: []DevContainerConfig{
				{
					RemoteUser:    "vscode",
					ContainerUser: "developer",
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				assert.Equal(t, "local-image", merged.Image)
				assert.Equal(t, "vscode", merged.RemoteUser)
				assert.Equal(t, "developer", merged.ContainerUser)
			},
		},
		{
			name: "boolean OR: init is true if any is true",
			local: &DevContainerConfig{
				Image: "alpine",
				// Init not set (nil)
			},
			imageConfigs: []DevContainerConfig{
				{
					Init: boolPtr(true),
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				require.NotNil(t, merged.Init)
				assert.True(t, *merged.Init)
			},
		},
		{
			name: "boolean OR: privileged is true if any is true",
			local: &DevContainerConfig{
				Image:      "alpine",
				Privileged: boolPtr(false),
			},
			imageConfigs: []DevContainerConfig{
				{
					Privileged: boolPtr(true),
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				// Spec: privileged is true if any source sets it, so an image
				// that needs it cannot be silently downgraded.
				require.NotNil(t, merged.Privileged)
				assert.True(t, *merged.Privileged)
			},
		},
		{
			name: "array union: capAdd",
			local: &DevContainerConfig{
				Image:  "alpine",
				CapAdd: []string{"SYS_PTRACE"},
			},
			imageConfigs: []DevContainerConfig{
				{
					CapAdd: []string{"NET_ADMIN", "SYS_PTRACE"},
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				assert.Contains(t, merged.CapAdd, "SYS_PTRACE")
				assert.Contains(t, merged.CapAdd, "NET_ADMIN")
				// Should deduplicate
				count := 0
				for _, cap := range merged.CapAdd {
					if cap == "SYS_PTRACE" {
						count++
					}
				}
				assert.Equal(t, 1, count, "SYS_PTRACE should appear only once")
			},
		},
		{
			name: "array union: securityOpt",
			local: &DevContainerConfig{
				Image:       "alpine",
				SecurityOpt: []string{"seccomp=unconfined"},
			},
			imageConfigs: []DevContainerConfig{
				{
					SecurityOpt: []string{"apparmor=unconfined"},
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				assert.Contains(t, merged.SecurityOpt, "seccomp=unconfined")
				assert.Contains(t, merged.SecurityOpt, "apparmor=unconfined")
			},
		},
		{
			name: "map merge: containerEnv",
			local: &DevContainerConfig{
				Image:        "alpine",
				ContainerEnv: map[string]string{"LOCAL_VAR": "local-value"},
			},
			imageConfigs: []DevContainerConfig{
				{
					ContainerEnv: map[string]string{
						"IMAGE_VAR": "image-value",
						"LOCAL_VAR": "should-not-override", // Local should win
					},
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				assert.Equal(t, "local-value", merged.ContainerEnv["LOCAL_VAR"])
				assert.Equal(t, "image-value", merged.ContainerEnv["IMAGE_VAR"])
			},
		},
		{
			name: "map merge: remoteEnv",
			local: &DevContainerConfig{
				Image:     "alpine",
				RemoteEnv: map[string]*string{"EDITOR": strPtr("vim"), "PAGER": nil},
			},
			imageConfigs: []DevContainerConfig{
				{
					RemoteEnv: map[string]*string{
						"TERM":   strPtr("xterm-256color"),
						"EDITOR": strPtr("nano"), // Should not override local
						"PAGER":  strPtr("less"), // unset by local null
					},
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				assert.Equal(t, "vim", *merged.RemoteEnv["EDITOR"])
				assert.Equal(t, "xterm-256color", *merged.RemoteEnv["TERM"])
				assert.Nil(t, merged.RemoteEnv["PAGER"], "a local null unsets the image's value")
			},
		},
		{
			name: "features merge",
			local: &DevContainerConfig{
				Image: "alpine",
				Features: map[string]interface{}{
					"local-feature": map[string]interface{}{"option": "value"},
				},
			},
			imageConfigs: []DevContainerConfig{
				{
					Features: map[string]interface{}{
						"image-feature": map[string]interface{}{},
						"local-feature": map[string]interface{}{"option": "should-not-override"},
					},
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				_, hasLocalFeature := merged.Features["local-feature"]
				_, hasImageFeature := merged.Features["image-feature"]
				assert.True(t, hasLocalFeature)
				assert.True(t, hasImageFeature)
			},
		},
		{
			name: "multiple image configs merge in order",
			local: &DevContainerConfig{
				Image: "local-image",
			},
			imageConfigs: []DevContainerConfig{
				{
					RemoteUser: "first-user",
					WaitFor:    "onCreateCommand",
				},
				{
					RemoteUser: "second-user", // This should be used (last wins before local)
					WaitFor:    "postCreateCommand",
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				// Later image layers override earlier ones (spec: last wins)
				assert.Equal(t, "second-user", merged.RemoteUser)
				assert.Equal(t, "postCreateCommand", merged.WaitFor)
			},
		},
		{
			name: "workspaceFolder from image if not in local",
			local: &DevContainerConfig{
				Image: "alpine",
			},
			imageConfigs: []DevContainerConfig{
				{
					WorkspaceFolder: "/home/vscode/workspace",
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				assert.Equal(t, "/home/vscode/workspace", merged.WorkspaceFolder)
			},
		},
		{
			name: "local workspaceFolder overrides image",
			local: &DevContainerConfig{
				Image:           "alpine",
				WorkspaceFolder: "/workspaces/myproject",
			},
			imageConfigs: []DevContainerConfig{
				{
					WorkspaceFolder: "/home/vscode/workspace",
				},
			},
			check: func(t *testing.T, merged *DevContainerConfig) {
				assert.Equal(t, "/workspaces/myproject", merged.WorkspaceFolder)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged := MergeMetadata(tt.local, tt.imageConfigs)
			require.NotNil(t, merged)
			if tt.check != nil {
				tt.check(t, merged)
			}
		})
	}
}

func TestUnionExtensions(t *testing.T) {
	tests := []struct {
		name   string
		target []interface{}
		source []interface{}
		want   []string
	}{
		{
			name:   "both empty",
			target: nil,
			source: nil,
			want:   nil,
		},
		{
			name:   "target only",
			target: []interface{}{"ext1", "ext2"},
			source: nil,
			want:   []string{"ext1", "ext2"},
		},
		{
			name:   "source only",
			target: nil,
			source: []interface{}{"ext1", "ext2"},
			want:   []string{"ext1", "ext2"},
		},
		{
			name:   "union without duplicates",
			target: []interface{}{"ext1", "ext2"},
			source: []interface{}{"ext2", "ext3"},
			want:   []string{"ext1", "ext2", "ext3"},
		},
		{
			name:   "preserves target order",
			target: []interface{}{"z-ext", "a-ext"},
			source: []interface{}{"m-ext"},
			want:   []string{"z-ext", "a-ext", "m-ext"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := unionExtensions(tt.target, tt.source)
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				require.Len(t, got, len(tt.want))
				for i, ext := range got {
					assert.Equal(t, tt.want[i], ext.(string))
				}
			}
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

// Merging must not mutate the caller's local config (its maps were
// previously shared with the merged result).
func TestMergeMetadataDoesNotMutateLocal(t *testing.T) {
	local := &DevContainerConfig{
		ContainerEnv:   map[string]string{"A": "local"},
		Customizations: map[string]interface{}{"vscode": map[string]interface{}{"extensions": []interface{}{"a"}}},
	}
	merged := MergeMetadata(local, []DevContainerConfig{{
		ContainerEnv:   map[string]string{"B": "image"},
		Customizations: map[string]interface{}{"vscode": map[string]interface{}{"extensions": []interface{}{"b"}}},
	}})

	assert.Equal(t, map[string]string{"A": "local", "B": "image"}, merged.ContainerEnv)
	assert.Equal(t, map[string]string{"A": "local"}, local.ContainerEnv)
	exts := local.Customizations["vscode"].(map[string]interface{})["extensions"].([]interface{})
	assert.Equal(t, []interface{}{"a"}, exts)
}

func TestMergeMetadataMountsLastWinsPerTarget(t *testing.T) {
	merged := MergeMetadata(&DevContainerConfig{
		Mounts: []Mount{{Source: "local-vol", Target: "/data", Type: "volume"}},
	}, []DevContainerConfig{{
		Mounts: []Mount{{Source: "img-vol", Target: "/data", Type: "volume"}, {Source: "other", Target: "/other", Type: "volume"}},
	}})
	require.Len(t, merged.Mounts, 2)
	assert.Equal(t, "local-vol", merged.Mounts[0].Source)
	assert.Equal(t, "/other", merged.Mounts[1].Target)
}

func strPtr(s string) *string { return &s }
