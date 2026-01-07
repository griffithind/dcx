package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, cfg *DevcontainerConfig)
	}{
		{
			name: "simple compose config",
			input: `{
				"name": "Test",
				"dockerComposeFile": "docker-compose.yml",
				"service": "app",
				"workspaceFolder": "/workspace"
			}`,
			wantErr: false,
			check: func(t *testing.T, cfg *DevcontainerConfig) {
				assert.Equal(t, "Test", cfg.Name)
				assert.Equal(t, "app", cfg.Service)
				assert.Equal(t, "/workspace", cfg.WorkspaceFolder)
				assert.True(t, cfg.IsComposePlan())
				assert.False(t, cfg.IsSinglePlan())
			},
		},
		{
			name: "image config",
			input: `{
				"name": "Image Test",
				"image": "node:18",
				"workspaceFolder": "/app"
			}`,
			wantErr: false,
			check: func(t *testing.T, cfg *DevcontainerConfig) {
				assert.Equal(t, "node:18", cfg.Image)
				assert.False(t, cfg.IsComposePlan())
				assert.True(t, cfg.IsSinglePlan())
			},
		},
		{
			name: "config with comments",
			input: `{
				// This is a comment
				"name": "Test",
				"image": "alpine",
				/* Multi-line
				   comment */
				"workspaceFolder": "/workspace"
			}`,
			wantErr: false,
			check: func(t *testing.T, cfg *DevcontainerConfig) {
				assert.Equal(t, "Test", cfg.Name)
				assert.Equal(t, "alpine", cfg.Image)
			},
		},
		{
			name: "config with trailing comma",
			input: `{
				"name": "Test",
				"image": "alpine",
			}`,
			wantErr: false,
			check: func(t *testing.T, cfg *DevcontainerConfig) {
				assert.Equal(t, "Test", cfg.Name)
			},
		},
		{
			name: "multiple compose files",
			input: `{
				"name": "Multi Compose",
				"dockerComposeFile": ["docker-compose.yml", "docker-compose.override.yml"],
				"service": "app"
			}`,
			wantErr: false,
			check: func(t *testing.T, cfg *DevcontainerConfig) {
				files := cfg.GetDockerComposeFiles()
				assert.Len(t, files, 2)
				assert.Equal(t, "docker-compose.yml", files[0])
				assert.Equal(t, "docker-compose.override.yml", files[1])
			},
		},
		{
			name: "config with environment",
			input: `{
				"name": "Env Test",
				"image": "alpine",
				"containerEnv": {
					"FOO": "bar",
					"BAZ": "qux"
				}
			}`,
			wantErr: false,
			check: func(t *testing.T, cfg *DevcontainerConfig) {
				assert.Equal(t, "bar", cfg.ContainerEnv["FOO"])
				assert.Equal(t, "qux", cfg.ContainerEnv["BAZ"])
			},
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			wantErr: true,
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
			require.NotNil(t, cfg)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestSubstitute(t *testing.T) {
	// Set test environment variable
	os.Setenv("TEST_VAR", "test_value")
	defer os.Unsetenv("TEST_VAR")

	ctx := &SubstitutionContext{
		LocalWorkspaceFolder:     "/home/user/project",
		ContainerWorkspaceFolder: "/workspace",
		DevcontainerID:           "test123",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "localEnv substitution",
			input:    "${localEnv:TEST_VAR}",
			expected: "test_value",
		},
		{
			name:     "env substitution",
			input:    "${env:TEST_VAR}",
			expected: "test_value",
		},
		{
			name:     "localEnv with default",
			input:    "${localEnv:NONEXISTENT:default}",
			expected: "default",
		},
		{
			name:     "localWorkspaceFolder",
			input:    "${localWorkspaceFolder}",
			expected: "/home/user/project",
		},
		{
			name:     "containerWorkspaceFolder",
			input:    "${containerWorkspaceFolder}",
			expected: "/workspace",
		},
		{
			name:     "localWorkspaceFolderBasename",
			input:    "${localWorkspaceFolderBasename}",
			expected: "project",
		},
		{
			name:     "devcontainerId",
			input:    "${devcontainerId}",
			expected: "test123",
		},
		{
			name:     "mixed substitution",
			input:    "${localWorkspaceFolder}/src/${localEnv:TEST_VAR}",
			expected: "/home/user/project/src/test_value",
		},
		{
			name:     "no substitution needed",
			input:    "plain string",
			expected: "plain string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Substitute(tt.input, ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetermineContainerWorkspaceFolder(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *DevcontainerConfig
		localWorkspace string
		expected       string
	}{
		{
			name:           "explicit workspace folder",
			cfg:            &DevcontainerConfig{WorkspaceFolder: "/app"},
			localWorkspace: "/home/user/myproject",
			expected:       "/app",
		},
		{
			name:           "default workspace folder",
			cfg:            &DevcontainerConfig{},
			localWorkspace: "/home/user/myproject",
			expected:       "/workspaces/myproject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetermineContainerWorkspaceFolder(tt.cfg, tt.localWorkspace)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolve(t *testing.T) {
	// Create temporary test directory
	tmpDir := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create devcontainer.json
	configPath := filepath.Join(devcontainerDir, "devcontainer.json")
	err = os.WriteFile(configPath, []byte(`{"name": "Test"}`), 0644)
	require.NoError(t, err)

	// Test resolution
	resolved, err := Resolve(tmpDir)
	require.NoError(t, err)
	assert.Equal(t, configPath, resolved)
}

func TestResolveNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := Resolve(tmpDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no devcontainer.json found")
}

// TestOuzoERPStyleConfig tests parsing of a full ouzoerp-style devcontainer.json
// with compose, features, remoteUser, containerEnv, forwardPorts, and lifecycle hooks.
func TestOuzoERPStyleConfig(t *testing.T) {
	input := `{
		"name": "OuzoERP development",
		"dockerComposeFile": "compose.yaml",
		"service": "app",
		"workspaceFolder": "/workspaces/${localWorkspaceFolderBasename}",
		"containerEnv": {
			"PGHOST": "db",
			"PGUSER": "postgres",
			"PGPASSWORD": "ouzo"
		},
		"forwardPorts": [3000],
		"onCreateCommand": ".devcontainer/boot.sh",
		"updateRemoteUserUID": true,
		"features": {
			"ghcr.io/devcontainers/features/common-utils:2": {
				"username": "${localEnv:USER}",
				"installZsh": false,
				"installOhMyZsh": false,
				"installOhMyZshConfig": false,
				"upgradePackages": false
			},
			"ghcr.io/devcontainers/features/docker-outside-of-docker:1": {
				"moby": false
			}
		},
		"remoteUser": "${localEnv:USER}"
	}`

	cfg, err := Parse([]byte(input))
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Basic config
	assert.Equal(t, "OuzoERP development", cfg.Name)
	assert.Equal(t, "app", cfg.Service)
	assert.True(t, cfg.IsComposePlan())
	assert.False(t, cfg.IsSinglePlan())

	// Compose file
	composeFiles := cfg.GetDockerComposeFiles()
	assert.Len(t, composeFiles, 1)
	assert.Equal(t, "compose.yaml", composeFiles[0])

	// Workspace folder with substitution variable
	assert.Equal(t, "/workspaces/${localWorkspaceFolderBasename}", cfg.WorkspaceFolder)

	// Container environment
	assert.Len(t, cfg.ContainerEnv, 3)
	assert.Equal(t, "db", cfg.ContainerEnv["PGHOST"])
	assert.Equal(t, "postgres", cfg.ContainerEnv["PGUSER"])
	assert.Equal(t, "ouzo", cfg.ContainerEnv["PGPASSWORD"])

	// Forward ports (GetForwardPorts returns []string in "port:port" format)
	ports := cfg.GetForwardPorts()
	assert.Contains(t, ports, "3000:3000")

	// Lifecycle hook
	assert.NotNil(t, cfg.OnCreateCommand)

	// Remote user with substitution variable
	assert.Equal(t, "${localEnv:USER}", cfg.RemoteUser)

	// Features
	assert.Len(t, cfg.Features, 2)
	assert.Contains(t, cfg.Features, "ghcr.io/devcontainers/features/common-utils:2")
	assert.Contains(t, cfg.Features, "ghcr.io/devcontainers/features/docker-outside-of-docker:1")
}

// TestWorkspaceFolderSubstitution tests that workspace folder variables are correctly substituted.
func TestWorkspaceFolderSubstitution(t *testing.T) {
	tests := []struct {
		name              string
		workspaceFolder   string
		localWorkspace    string
		expectedContainer string
	}{
		{
			name:              "localWorkspaceFolderBasename substitution",
			workspaceFolder:   "/workspaces/${localWorkspaceFolderBasename}",
			localWorkspace:    "/Users/griffithind/ouzoerp/src",
			expectedContainer: "/workspaces/src",
		},
		{
			name:              "nested path basename",
			workspaceFolder:   "/app/${localWorkspaceFolderBasename}",
			localWorkspace:    "/home/user/projects/myapp",
			expectedContainer: "/app/myapp",
		},
		{
			name:              "no substitution needed",
			workspaceFolder:   "/workspace",
			localWorkspace:    "/home/user/project",
			expectedContainer: "/workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use Substitute function to perform variable substitution
			ctx := &SubstitutionContext{
				LocalWorkspaceFolder: tt.localWorkspace,
			}
			result := Substitute(tt.workspaceFolder, ctx)
			assert.Equal(t, tt.expectedContainer, result)
		})
	}
}

// TestRemoteUserSubstitution tests that remoteUser is correctly substituted.
func TestRemoteUserSubstitution(t *testing.T) {
	// Set test environment variable
	os.Setenv("USER", "testuser")
	defer os.Unsetenv("USER")

	ctx := &SubstitutionContext{
		LocalWorkspaceFolder: "/home/testuser/project",
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "localEnv:USER substitution",
			input:    "${localEnv:USER}",
			expected: "testuser",
		},
		{
			name:     "static user",
			input:    "vscode",
			expected: "vscode",
		},
		{
			name:     "localEnv with fallback",
			input:    "${localEnv:NONEXISTENT_USER:defaultuser}",
			expected: "defaultuser",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Substitute(tt.input, ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSubstituteConfigRemoteUser tests that SubstituteConfig substitutes remoteUser.
func TestSubstituteConfigRemoteUser(t *testing.T) {
	// Set test environment variable
	os.Setenv("USER", "testuser")
	defer os.Unsetenv("USER")

	ctx := &SubstitutionContext{
		LocalWorkspaceFolder: "/home/testuser/project",
	}

	cfg := &DevcontainerConfig{
		RemoteUser: "${localEnv:USER}",
	}

	SubstituteConfig(cfg, ctx)

	assert.Equal(t, "testuser", cfg.RemoteUser)
}

// TestForwardPortsParsing tests various forward ports formats.
// GetForwardPorts returns []string in "port:port" format for numbers.
func TestForwardPortsParsing(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedPorts []string
	}{
		{
			name: "single port as number",
			input: `{
				"image": "alpine",
				"forwardPorts": [3000]
			}`,
			expectedPorts: []string{"3000:3000"},
		},
		{
			name: "multiple ports",
			input: `{
				"image": "alpine",
				"forwardPorts": [3000, 5432, 6379]
			}`,
			expectedPorts: []string{"3000:3000", "5432:5432", "6379:6379"},
		},
		{
			name: "port as string",
			input: `{
				"image": "alpine",
				"forwardPorts": ["3000"]
			}`,
			expectedPorts: []string{"3000"},
		},
		{
			name: "port with label",
			input: `{
				"image": "alpine",
				"forwardPorts": ["3000:web", 5432]
			}`,
			expectedPorts: []string{"3000:web", "5432:5432"},
		},
		{
			name: "no forward ports",
			input: `{
				"image": "alpine"
			}`,
			expectedPorts: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.input))
			require.NoError(t, err)
			ports := cfg.GetForwardPorts()
			assert.Equal(t, tt.expectedPorts, ports)
		})
	}
}

// TestFeaturesParsing tests parsing of various feature formats.
func TestFeaturesParsing(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectedCount  int
		checkFeatures  func(t *testing.T, features map[string]interface{})
	}{
		{
			name: "OCI features with options",
			input: `{
				"image": "alpine",
				"features": {
					"ghcr.io/devcontainers/features/common-utils:2": {
						"username": "testuser",
						"installZsh": false
					},
					"ghcr.io/devcontainers/features/docker-outside-of-docker:1": {
						"moby": false
					}
				}
			}`,
			expectedCount: 2,
			checkFeatures: func(t *testing.T, features map[string]interface{}) {
				commonUtils := features["ghcr.io/devcontainers/features/common-utils:2"]
				assert.NotNil(t, commonUtils)

				// Check options are map[string]interface{}
				opts, ok := commonUtils.(map[string]interface{})
				require.True(t, ok)
				assert.Equal(t, "testuser", opts["username"])
				assert.Equal(t, false, opts["installZsh"])
			},
		},
		{
			name: "feature with empty options",
			input: `{
				"image": "alpine",
				"features": {
					"ghcr.io/devcontainers/features/git:1": {}
				}
			}`,
			expectedCount: 1,
			checkFeatures: func(t *testing.T, features map[string]interface{}) {
				git := features["ghcr.io/devcontainers/features/git:1"]
				assert.NotNil(t, git)
			},
		},
		{
			name: "short form feature reference",
			input: `{
				"image": "alpine",
				"features": {
					"devcontainers/features/go:1": {
						"version": "1.21"
					}
				}
			}`,
			expectedCount: 1,
			checkFeatures: func(t *testing.T, features map[string]interface{}) {
				assert.Contains(t, features, "devcontainers/features/go:1")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.input))
			require.NoError(t, err)
			assert.Len(t, cfg.Features, tt.expectedCount)
			if tt.checkFeatures != nil {
				tt.checkFeatures(t, cfg.Features)
			}
		})
	}
}

// TestLifecycleHooksParsing tests parsing of various lifecycle hook formats.
func TestLifecycleHooksParsing(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		hookField string
		isNil     bool
	}{
		{
			name: "onCreateCommand as string",
			input: `{
				"image": "alpine",
				"onCreateCommand": ".devcontainer/boot.sh"
			}`,
			hookField: "onCreate",
			isNil:     false,
		},
		{
			name: "postCreateCommand as string",
			input: `{
				"image": "alpine",
				"postCreateCommand": "npm install"
			}`,
			hookField: "postCreate",
			isNil:     false,
		},
		{
			name: "postStartCommand as array",
			input: `{
				"image": "alpine",
				"postStartCommand": ["echo", "hello"]
			}`,
			hookField: "postStart",
			isNil:     false,
		},
		{
			name: "updateContentCommand",
			input: `{
				"image": "alpine",
				"updateContentCommand": "git pull"
			}`,
			hookField: "updateContent",
			isNil:     false,
		},
		{
			name: "no hooks",
			input: `{
				"image": "alpine"
			}`,
			hookField: "onCreate",
			isNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Parse([]byte(tt.input))
			require.NoError(t, err)

			var hook interface{}
			switch tt.hookField {
			case "onCreate":
				hook = cfg.OnCreateCommand
			case "postCreate":
				hook = cfg.PostCreateCommand
			case "postStart":
				hook = cfg.PostStartCommand
			case "updateContent":
				hook = cfg.UpdateContentCommand
			}

			if tt.isNil {
				assert.Nil(t, hook)
			} else {
				assert.NotNil(t, hook)
			}
		})
	}
}

// TestContainerEnvWithSubstitution tests that containerEnv values with variables are preserved.
func TestContainerEnvWithSubstitution(t *testing.T) {
	input := `{
		"image": "alpine",
		"containerEnv": {
			"APP_ENV": "development",
			"USER_HOME": "/home/${localEnv:USER}",
			"STATIC_VAR": "static_value"
		}
	}`

	cfg, err := Parse([]byte(input))
	require.NoError(t, err)

	// Variables should be preserved in the parsed config (not substituted yet)
	assert.Equal(t, "development", cfg.ContainerEnv["APP_ENV"])
	assert.Equal(t, "/home/${localEnv:USER}", cfg.ContainerEnv["USER_HOME"])
	assert.Equal(t, "static_value", cfg.ContainerEnv["STATIC_VAR"])
}

// TestRunServicesConfig tests parsing of runServices configuration.
func TestRunServicesConfig(t *testing.T) {
	input := `{
		"dockerComposeFile": "compose.yaml",
		"service": "app",
		"runServices": ["app", "db", "redis"]
	}`

	cfg, err := Parse([]byte(input))
	require.NoError(t, err)

	assert.Equal(t, []string{"app", "db", "redis"}, cfg.RunServices)
}

// TestNewDevcontainerFields tests parsing of the newly added devcontainer specification fields.
func TestNewDevcontainerFields(t *testing.T) {
	t.Run("updateRemoteUserUID", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected *bool
		}{
			{
				name: "updateRemoteUserUID true",
				input: `{
					"image": "alpine",
					"updateRemoteUserUID": true
				}`,
				expected: boolPtr(true),
			},
			{
				name: "updateRemoteUserUID false",
				input: `{
					"image": "alpine",
					"updateRemoteUserUID": false
				}`,
				expected: boolPtr(false),
			},
			{
				name: "updateRemoteUserUID not set",
				input: `{
					"image": "alpine"
				}`,
				expected: nil,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg, err := Parse([]byte(tt.input))
				require.NoError(t, err)
				if tt.expected == nil {
					assert.Nil(t, cfg.UpdateRemoteUserUID)
				} else {
					require.NotNil(t, cfg.UpdateRemoteUserUID)
					assert.Equal(t, *tt.expected, *cfg.UpdateRemoteUserUID)
				}
			})
		}
	})

	t.Run("userEnvProbe", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name: "userEnvProbe none",
				input: `{
					"image": "alpine",
					"userEnvProbe": "none"
				}`,
				expected: "none",
			},
			{
				name: "userEnvProbe loginShell",
				input: `{
					"image": "alpine",
					"userEnvProbe": "loginShell"
				}`,
				expected: "loginShell",
			},
			{
				name: "userEnvProbe loginInteractiveShell",
				input: `{
					"image": "alpine",
					"userEnvProbe": "loginInteractiveShell"
				}`,
				expected: "loginInteractiveShell",
			},
			{
				name: "userEnvProbe interactiveShell",
				input: `{
					"image": "alpine",
					"userEnvProbe": "interactiveShell"
				}`,
				expected: "interactiveShell",
			},
			{
				name: "userEnvProbe not set",
				input: `{
					"image": "alpine"
				}`,
				expected: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg, err := Parse([]byte(tt.input))
				require.NoError(t, err)
				assert.Equal(t, tt.expected, cfg.UserEnvProbe)
			})
		}
	})

	t.Run("waitFor", func(t *testing.T) {
		tests := []struct {
			name     string
			input    string
			expected string
		}{
			{
				name: "waitFor onCreateCommand",
				input: `{
					"image": "alpine",
					"waitFor": "onCreateCommand"
				}`,
				expected: "onCreateCommand",
			},
			{
				name: "waitFor updateContentCommand",
				input: `{
					"image": "alpine",
					"waitFor": "updateContentCommand"
				}`,
				expected: "updateContentCommand",
			},
			{
				name: "waitFor postCreateCommand",
				input: `{
					"image": "alpine",
					"waitFor": "postCreateCommand"
				}`,
				expected: "postCreateCommand",
			},
			{
				name: "waitFor postStartCommand",
				input: `{
					"image": "alpine",
					"waitFor": "postStartCommand"
				}`,
				expected: "postStartCommand",
			},
			{
				name: "waitFor not set",
				input: `{
					"image": "alpine"
				}`,
				expected: "",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg, err := Parse([]byte(tt.input))
				require.NoError(t, err)
				assert.Equal(t, tt.expected, cfg.WaitFor)
			})
		}
	})

	t.Run("otherPortsAttributes", func(t *testing.T) {
		tests := []struct {
			name        string
			input       string
			expectNil   bool
			checkAttrs  func(t *testing.T, attrs interface{})
		}{
			{
				name: "otherPortsAttributes with onAutoForward",
				input: `{
					"image": "alpine",
					"otherPortsAttributes": {
						"onAutoForward": "ignore"
					}
				}`,
				expectNil: false,
				checkAttrs: func(t *testing.T, attrs interface{}) {
					attrMap, ok := attrs.(map[string]interface{})
					require.True(t, ok, "otherPortsAttributes should be a map")
					assert.Equal(t, "ignore", attrMap["onAutoForward"])
				},
			},
			{
				name: "otherPortsAttributes with label and protocol",
				input: `{
					"image": "alpine",
					"otherPortsAttributes": {
						"label": "Other Ports",
						"protocol": "http",
						"onAutoForward": "notify"
					}
				}`,
				expectNil: false,
				checkAttrs: func(t *testing.T, attrs interface{}) {
					attrMap, ok := attrs.(map[string]interface{})
					require.True(t, ok, "otherPortsAttributes should be a map")
					assert.Equal(t, "Other Ports", attrMap["label"])
					assert.Equal(t, "http", attrMap["protocol"])
					assert.Equal(t, "notify", attrMap["onAutoForward"])
				},
			},
			{
				name: "otherPortsAttributes not set",
				input: `{
					"image": "alpine"
				}`,
				expectNil: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				cfg, err := Parse([]byte(tt.input))
				require.NoError(t, err)
				if tt.expectNil {
					assert.Nil(t, cfg.OtherPortsAttributes)
				} else {
					require.NotNil(t, cfg.OtherPortsAttributes)
					if tt.checkAttrs != nil {
						tt.checkAttrs(t, cfg.OtherPortsAttributes)
					}
				}
			})
		}
	})

	t.Run("all_new_fields_together", func(t *testing.T) {
		input := `{
			"name": "Full config test",
			"image": "node:18",
			"remoteUser": "vscode",
			"updateRemoteUserUID": true,
			"userEnvProbe": "loginInteractiveShell",
			"waitFor": "postCreateCommand",
			"forwardPorts": [3000, 5432],
			"portsAttributes": {
				"3000": { "label": "Web", "onAutoForward": "notify" }
			},
			"otherPortsAttributes": {
				"onAutoForward": "silent"
			},
			"onCreateCommand": "npm install",
			"postCreateCommand": "npm run build"
		}`

		cfg, err := Parse([]byte(input))
		require.NoError(t, err)

		// Verify all new fields
		require.NotNil(t, cfg.UpdateRemoteUserUID)
		assert.True(t, *cfg.UpdateRemoteUserUID)
		assert.Equal(t, "loginInteractiveShell", cfg.UserEnvProbe)
		assert.Equal(t, "postCreateCommand", cfg.WaitFor)
		require.NotNil(t, cfg.OtherPortsAttributes)
		attrMap, ok := cfg.OtherPortsAttributes.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "silent", attrMap["onAutoForward"])
	})
}

// boolPtr returns a pointer to a bool value.
func boolPtr(b bool) *bool {
	return &b
}

// TestOverrideCommandAndShutdownAction tests parsing of overrideCommand and shutdownAction fields.
func TestOverrideCommandAndShutdownAction(t *testing.T) {
	t.Run("overrideCommand true", func(t *testing.T) {
		input := `{
			"image": "alpine",
			"overrideCommand": true
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		require.NotNil(t, cfg.OverrideCommand)
		assert.True(t, *cfg.OverrideCommand)
	})

	t.Run("overrideCommand false", func(t *testing.T) {
		input := `{
			"image": "alpine",
			"overrideCommand": false
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		require.NotNil(t, cfg.OverrideCommand)
		assert.False(t, *cfg.OverrideCommand)
	})

	t.Run("overrideCommand not set", func(t *testing.T) {
		input := `{
			"image": "alpine"
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		assert.Nil(t, cfg.OverrideCommand)
	})

	t.Run("shutdownAction stopContainer", func(t *testing.T) {
		input := `{
			"image": "alpine",
			"shutdownAction": "stopContainer"
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, "stopContainer", cfg.ShutdownAction)
	})

	t.Run("shutdownAction none", func(t *testing.T) {
		input := `{
			"image": "alpine",
			"shutdownAction": "none"
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, "none", cfg.ShutdownAction)
	})

	t.Run("shutdownAction not set", func(t *testing.T) {
		input := `{
			"image": "alpine"
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, "", cfg.ShutdownAction)
	})

	t.Run("both fields together", func(t *testing.T) {
		input := `{
			"image": "alpine",
			"overrideCommand": true,
			"shutdownAction": "none"
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		require.NotNil(t, cfg.OverrideCommand)
		assert.True(t, *cfg.OverrideCommand)
		assert.Equal(t, "none", cfg.ShutdownAction)
	})
}

// TestHostRequirementsParsing tests parsing of hostRequirements field.
func TestHostRequirementsParsing(t *testing.T) {
	t.Run("all requirements", func(t *testing.T) {
		input := `{
			"image": "alpine",
			"hostRequirements": {
				"cpus": 4,
				"memory": "8gb",
				"storage": "50gb",
				"gpu": true
			}
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		require.NotNil(t, cfg.HostRequirements)
		assert.Equal(t, 4, cfg.HostRequirements.CPUs)
		assert.Equal(t, "8gb", cfg.HostRequirements.Memory)
		assert.Equal(t, "50gb", cfg.HostRequirements.Storage)
		assert.Equal(t, true, cfg.HostRequirements.GPU)
	})

	t.Run("GPU optional", func(t *testing.T) {
		input := `{
			"image": "alpine",
			"hostRequirements": {
				"gpu": "optional"
			}
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		require.NotNil(t, cfg.HostRequirements)
		assert.Equal(t, "optional", cfg.HostRequirements.GPU)
	})

	t.Run("GPU object", func(t *testing.T) {
		input := `{
			"image": "alpine",
			"hostRequirements": {
				"gpu": {
					"cores": 4,
					"memory": "8gb"
				}
			}
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		require.NotNil(t, cfg.HostRequirements)
		gpuMap, ok := cfg.HostRequirements.GPU.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, float64(4), gpuMap["cores"])
	})

	t.Run("no host requirements", func(t *testing.T) {
		input := `{
			"image": "alpine"
		}`
		cfg, err := Parse([]byte(input))
		require.NoError(t, err)
		assert.Nil(t, cfg.HostRequirements)
	})
}
