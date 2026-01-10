package devcontainer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDevcontainerJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(*testing.T, *DevContainerConfig)
	}{
		{
			name:  "simple image config",
			input: `{"image": "alpine:latest"}`,
			check: func(t *testing.T, cfg *DevContainerConfig) {
				assert.Equal(t, "alpine:latest", cfg.Image)
			},
		},
		{
			name:  "with name",
			input: `{"name": "my-project", "image": "ubuntu"}`,
			check: func(t *testing.T, cfg *DevContainerConfig) {
				assert.Equal(t, "my-project", cfg.Name)
				assert.Equal(t, "ubuntu", cfg.Image)
			},
		},
		{
			name:  "JSONC with comments",
			input: `{"image": "alpine" /* comment */, "name": "test" // inline comment
			}`,
			check: func(t *testing.T, cfg *DevContainerConfig) {
				assert.Equal(t, "alpine", cfg.Image)
				assert.Equal(t, "test", cfg.Name)
			},
		},
		{
			name:  "JSONC with trailing comma",
			input: `{"image": "alpine", "name": "test",}`,
			check: func(t *testing.T, cfg *DevContainerConfig) {
				assert.Equal(t, "alpine", cfg.Image)
			},
		},
		{
			name:    "invalid JSON",
			input:   `{invalid}`,
			wantErr: true,
		},
		{
			name:  "with build config",
			input: `{"build": {"dockerfile": "Dockerfile", "context": "."}}`,
			check: func(t *testing.T, cfg *DevContainerConfig) {
				require.NotNil(t, cfg.Build)
				assert.Equal(t, "Dockerfile", cfg.Build.Dockerfile)
				assert.Equal(t, ".", cfg.Build.Context)
			},
		},
		{
			name:  "with compose config array",
			input: `{"dockerComposeFile": ["docker-compose.yml"], "service": "app"}`,
			check: func(t *testing.T, cfg *DevContainerConfig) {
				// Use getter since DockerComposeFile is interface{} type
				files := cfg.GetDockerComposeFiles()
				assert.Equal(t, []string{"docker-compose.yml"}, files)
				assert.Equal(t, "app", cfg.Service)
			},
		},
		{
			name:  "with remoteUser and containerUser",
			input: `{"image": "alpine", "remoteUser": "vscode", "containerUser": "developer"}`,
			check: func(t *testing.T, cfg *DevContainerConfig) {
				assert.Equal(t, "vscode", cfg.RemoteUser)
				assert.Equal(t, "developer", cfg.ContainerUser)
			},
		},
		{
			name:  "with containerEnv and remoteEnv",
			input: `{"image": "alpine", "containerEnv": {"FOO": "bar"}, "remoteEnv": {"BAZ": "qux"}}`,
			check: func(t *testing.T, cfg *DevContainerConfig) {
				assert.Equal(t, map[string]string{"FOO": "bar"}, cfg.ContainerEnv)
				assert.Equal(t, map[string]string{"BAZ": "qux"}, cfg.RemoteEnv)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.input))
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, dir string)
		wantPath   string
		wantErr    bool
		errContains string
	}{
		{
			name: "finds .devcontainer/devcontainer.json",
			setup: func(t *testing.T, dir string) {
				devDir := filepath.Join(dir, ".devcontainer")
				require.NoError(t, os.MkdirAll(devDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(devDir, "devcontainer.json"), []byte(`{}`), 0644))
			},
			wantPath: ".devcontainer/devcontainer.json",
		},
		{
			name: "finds .devcontainer.json at root",
			setup: func(t *testing.T, dir string) {
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".devcontainer.json"), []byte(`{}`), 0644))
			},
			wantPath: ".devcontainer.json",
		},
		{
			name: "prefers standard location over root",
			setup: func(t *testing.T, dir string) {
				// Create both, should prefer .devcontainer/devcontainer.json
				devDir := filepath.Join(dir, ".devcontainer")
				require.NoError(t, os.MkdirAll(devDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(devDir, "devcontainer.json"), []byte(`{}`), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(dir, ".devcontainer.json"), []byte(`{}`), 0644))
			},
			wantPath: ".devcontainer/devcontainer.json",
		},
		{
			name: "finds custom named json in .devcontainer",
			setup: func(t *testing.T, dir string) {
				devDir := filepath.Join(dir, ".devcontainer")
				require.NoError(t, os.MkdirAll(devDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(devDir, "custom.json"), []byte(`{}`), 0644))
			},
			wantPath: ".devcontainer/custom.json",
		},
		{
			name: "finds single multi-folder config",
			setup: func(t *testing.T, dir string) {
				pythonDir := filepath.Join(dir, ".devcontainer", "python")
				require.NoError(t, os.MkdirAll(pythonDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(pythonDir, "devcontainer.json"), []byte(`{}`), 0644))
			},
			wantPath: ".devcontainer/python/devcontainer.json",
		},
		{
			name: "errors on multiple multi-folder configs",
			setup: func(t *testing.T, dir string) {
				// Create multiple folder configurations
				pythonDir := filepath.Join(dir, ".devcontainer", "python")
				nodeDir := filepath.Join(dir, ".devcontainer", "node")
				require.NoError(t, os.MkdirAll(pythonDir, 0755))
				require.NoError(t, os.MkdirAll(nodeDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(pythonDir, "devcontainer.json"), []byte(`{}`), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(nodeDir, "devcontainer.json"), []byte(`{}`), 0644))
			},
			wantErr:     true,
			errContains: "multiple devcontainer configurations found",
		},
		{
			name:        "errors on non-existent workspace",
			setup:       func(t *testing.T, dir string) {},
			wantErr:     true,
			errContains: "workspace directory does not exist",
		},
		{
			name: "errors when no config found",
			setup: func(t *testing.T, dir string) {
				// Empty directory, no devcontainer config
			},
			wantErr:     true,
			errContains: "no devcontainer.json found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory for test
			var workDir string
			if tt.name == "errors on non-existent workspace" {
				workDir = "/nonexistent/path/that/does/not/exist"
			} else {
				dir := t.TempDir()
				workDir = dir
				tt.setup(t, dir)
			}

			got, err := Resolve(workDir)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			// Check the path ends with expected suffix
			assert.True(t, filepath.Base(got) == filepath.Base(tt.wantPath) ||
				filepath.Base(filepath.Dir(got)) == filepath.Base(filepath.Dir(tt.wantPath)),
				"expected path ending in %s, got %s", tt.wantPath, got)
		})
	}
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, dir string)
		configPath string
		wantErr    bool
		check      func(*testing.T, *DevContainerConfig)
	}{
		{
			name: "loads config with variable substitution",
			setup: func(t *testing.T, dir string) {
				devDir := filepath.Join(dir, ".devcontainer")
				require.NoError(t, os.MkdirAll(devDir, 0755))
				config := `{"image": "alpine", "workspaceFolder": "/workspaces/test"}`
				require.NoError(t, os.WriteFile(filepath.Join(devDir, "devcontainer.json"), []byte(config), 0644))
			},
			check: func(t *testing.T, cfg *DevContainerConfig) {
				assert.Equal(t, "alpine", cfg.Image)
				assert.Equal(t, "/workspaces/test", cfg.WorkspaceFolder)
			},
		},
		{
			name: "uses specified config path",
			setup: func(t *testing.T, dir string) {
				customDir := filepath.Join(dir, "custom")
				require.NoError(t, os.MkdirAll(customDir, 0755))
				config := `{"image": "custom-image"}`
				require.NoError(t, os.WriteFile(filepath.Join(customDir, "my-config.json"), []byte(config), 0644))
			},
			configPath: "custom/my-config.json",
			check: func(t *testing.T, cfg *DevContainerConfig) {
				assert.Equal(t, "custom-image", cfg.Image)
			},
		},
		{
			name: "handles relative config path",
			setup: func(t *testing.T, dir string) {
				devDir := filepath.Join(dir, ".devcontainer")
				require.NoError(t, os.MkdirAll(devDir, 0755))
				config := `{"image": "relative-test"}`
				require.NoError(t, os.WriteFile(filepath.Join(devDir, "devcontainer.json"), []byte(config), 0644))
			},
			configPath: ".devcontainer/devcontainer.json",
			check: func(t *testing.T, cfg *DevContainerConfig) {
				assert.Equal(t, "relative-test", cfg.Image)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)

			cfg, _, err := Load(dir, tt.configPath)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestResolveRelativePath(t *testing.T) {
	tests := []struct {
		name string
		base string
		path string
		want string
	}{
		{
			name: "absolute path unchanged",
			base: "/home/user",
			path: "/absolute/path",
			want: "/absolute/path",
		},
		{
			name: "relative path joined",
			base: "/home/user",
			path: "relative/path",
			want: "/home/user/relative/path",
		},
		{
			name: "dot path",
			base: "/home/user",
			path: "./local",
			want: "/home/user/local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveRelativePath(tt.base, tt.path)
			// Use filepath.Clean to normalize for comparison
			assert.Equal(t, filepath.Clean(tt.want), filepath.Clean(got))
		})
	}
}
