// Package conformance contains tests that verify Dev Container specification compliance.
// See: https://containers.dev/implementors/spec/
package conformance

import (
	"encoding/json"
	"testing"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSpecPropertyParsing verifies that all spec properties are correctly parsed.
func TestSpecPropertyParsing(t *testing.T) {
	t.Run("orchestration properties", func(t *testing.T) {
		input := `{
			"image": "mcr.microsoft.com/devcontainers/base:ubuntu",
			"build": {
				"dockerfile": "Dockerfile",
				"context": ".",
				"args": {"VERSION": "1.0"},
				"target": "dev",
				"cacheFrom": ["image:tag"],
				"options": ["--build-arg", "FOO=bar"]
			}
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.Equal(t, "mcr.microsoft.com/devcontainers/base:ubuntu", cfg.Image)
		require.NotNil(t, cfg.Build)
		assert.Equal(t, "Dockerfile", cfg.Build.Dockerfile)
		assert.Equal(t, ".", cfg.Build.Context)
		assert.Equal(t, "1.0", cfg.Build.Args["VERSION"])
		assert.Equal(t, "dev", cfg.Build.Target)
		assert.Equal(t, []string{"image:tag"}, cfg.Build.CacheFrom)
		assert.Equal(t, []string{"--build-arg", "FOO=bar"}, cfg.Build.Options)
	})

	t.Run("compose properties", func(t *testing.T) {
		input := `{
			"dockerComposeFile": ["docker-compose.yml", "docker-compose.override.yml"],
			"service": "app",
			"runServices": ["app", "db"]
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.Equal(t, []string{"docker-compose.yml", "docker-compose.override.yml"}, cfg.GetDockerComposeFiles())
		assert.Equal(t, "app", cfg.Service)
		assert.Equal(t, []string{"app", "db"}, cfg.RunServices)
		assert.True(t, cfg.IsComposePlan())
	})

	t.Run("workspace properties", func(t *testing.T) {
		input := `{
			"workspaceFolder": "/workspace",
			"workspaceMount": "source=${localWorkspaceFolder},target=/workspace,type=bind"
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.Equal(t, "/workspace", cfg.WorkspaceFolder)
		assert.Contains(t, cfg.WorkspaceMount, "source=")
	})

	t.Run("user properties", func(t *testing.T) {
		input := `{
			"remoteUser": "vscode",
			"containerUser": "root",
			"updateRemoteUserUID": true
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.Equal(t, "vscode", cfg.RemoteUser)
		assert.Equal(t, "root", cfg.ContainerUser)
		require.NotNil(t, cfg.UpdateRemoteUserUID)
		assert.True(t, *cfg.UpdateRemoteUserUID)
	})

	t.Run("environment properties", func(t *testing.T) {
		input := `{
			"containerEnv": {"FOO": "bar", "BAZ": "qux"},
			"remoteEnv": {"PATH": "/custom:${containerEnv:PATH}"}
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.Equal(t, "bar", cfg.ContainerEnv["FOO"])
		assert.Contains(t, cfg.RemoteEnv["PATH"], "${containerEnv:PATH}")
	})

	t.Run("port properties", func(t *testing.T) {
		input := `{
			"forwardPorts": [3000, "8080:80"],
			"appPort": [3000, 5000],
			"portsAttributes": {
				"3000": {
					"label": "Web",
					"protocol": "http",
					"onAutoForward": "notify",
					"requireLocalPort": true,
					"elevateIfNeeded": true
				}
			},
			"otherPortsAttributes": {"onAutoForward": "silent"}
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		ports := cfg.GetForwardPorts()
		assert.Equal(t, []string{"3000:3000", "8080:80"}, ports)

		appPorts := cfg.GetAppPorts()
		assert.Equal(t, []string{"3000:3000", "5000:5000"}, appPorts)

		attr := cfg.GetPortAttribute("3000")
		require.NotNil(t, attr)
		assert.Equal(t, "Web", attr.Label)
		assert.Equal(t, "http", attr.Protocol)
		assert.Equal(t, "notify", attr.OnAutoForward)
		assert.True(t, attr.RequireLocalPort)
		assert.True(t, attr.ElevateIfNeeded)
	})

	t.Run("mount properties - string format", func(t *testing.T) {
		input := `{
			"mounts": [
				"type=bind,source=/host,target=/container",
				"source=/data,target=/mnt,readonly"
			]
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		require.Len(t, cfg.Mounts, 2)
		assert.Equal(t, "/host", cfg.Mounts[0].Source)
		assert.Equal(t, "/container", cfg.Mounts[0].Target)
		assert.Equal(t, "bind", cfg.Mounts[0].Type)
		assert.True(t, cfg.Mounts[1].ReadOnly)
	})

	t.Run("mount properties - object format", func(t *testing.T) {
		input := `{
			"mounts": [
				{"source": "/host", "target": "/container", "type": "bind"},
				{"source": "/data", "target": "/mnt", "readonly": true}
			]
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		require.Len(t, cfg.Mounts, 2)
		assert.Equal(t, "/host", cfg.Mounts[0].Source)
		assert.Equal(t, "/container", cfg.Mounts[0].Target)
		assert.Equal(t, "bind", cfg.Mounts[0].Type)
		assert.True(t, cfg.Mounts[1].ReadOnly)
	})

	t.Run("runtime properties", func(t *testing.T) {
		input := `{
			"runArgs": ["--cpus", "2", "--memory", "4g"],
			"overrideCommand": true,
			"shutdownAction": "stopContainer",
			"init": true,
			"privileged": true,
			"capAdd": ["SYS_PTRACE", "NET_ADMIN"],
			"securityOpt": ["seccomp=unconfined"]
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.Equal(t, []string{"--cpus", "2", "--memory", "4g"}, cfg.RunArgs)
		require.NotNil(t, cfg.OverrideCommand)
		assert.True(t, *cfg.OverrideCommand)
		assert.Equal(t, "stopContainer", cfg.ShutdownAction)
		require.NotNil(t, cfg.Init)
		assert.True(t, *cfg.Init)
		require.NotNil(t, cfg.Privileged)
		assert.True(t, *cfg.Privileged)
		assert.Equal(t, []string{"SYS_PTRACE", "NET_ADMIN"}, cfg.CapAdd)
		assert.Equal(t, []string{"seccomp=unconfined"}, cfg.SecurityOpt)
	})

	t.Run("lifecycle properties", func(t *testing.T) {
		input := `{
			"initializeCommand": "echo init",
			"onCreateCommand": {"task1": "echo create1", "task2": "echo create2"},
			"updateContentCommand": ["echo", "update"],
			"postCreateCommand": "echo postCreate",
			"postStartCommand": "echo postStart",
			"postAttachCommand": "echo postAttach",
			"waitFor": "postCreateCommand"
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.Equal(t, "echo init", cfg.InitializeCommand)
		assert.NotNil(t, cfg.OnCreateCommand)
		assert.NotNil(t, cfg.UpdateContentCommand)
		assert.Equal(t, "echo postCreate", cfg.PostCreateCommand)
		assert.Equal(t, "echo postStart", cfg.PostStartCommand)
		assert.Equal(t, "echo postAttach", cfg.PostAttachCommand)
		assert.Equal(t, "postCreateCommand", cfg.WaitFor)
	})

	t.Run("host requirements", func(t *testing.T) {
		input := `{
			"hostRequirements": {
				"cpus": 4,
				"memory": "8gb",
				"storage": "32gb",
				"gpu": true
			}
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		require.NotNil(t, cfg.HostRequirements)
		assert.Equal(t, 4, cfg.HostRequirements.CPUs)
		assert.Equal(t, "8gb", cfg.HostRequirements.Memory)
		assert.Equal(t, "32gb", cfg.HostRequirements.Storage)
		assert.Equal(t, true, cfg.HostRequirements.GPU)
	})

	t.Run("userEnvProbe property", func(t *testing.T) {
		input := `{"userEnvProbe": "loginInteractiveShell"}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.Equal(t, "loginInteractiveShell", cfg.UserEnvProbe)
	})

	t.Run("features property", func(t *testing.T) {
		input := `{
			"features": {
				"ghcr.io/devcontainers/features/git:1": {},
				"ghcr.io/devcontainers/features/node:1": {"version": "18"}
			},
			"overrideFeatureInstallOrder": ["ghcr.io/devcontainers/features/git:1"]
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		require.Len(t, cfg.Features, 2)
		assert.Contains(t, cfg.Features, "ghcr.io/devcontainers/features/git:1")
		assert.Contains(t, cfg.Features, "ghcr.io/devcontainers/features/node:1")
		assert.Equal(t, []string{"ghcr.io/devcontainers/features/git:1"}, cfg.OverrideFeatureInstallOrder)
	})

	t.Run("customizations property", func(t *testing.T) {
		input := `{
			"customizations": {
				"vscode": {
					"extensions": ["ms-python.python"],
					"settings": {"python.linting.enabled": true}
				}
			}
		}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		require.NotNil(t, cfg.Customizations)
		assert.Contains(t, cfg.Customizations, "vscode")
	})
}

// TestVariableSubstitution verifies all variable substitution patterns.
func TestVariableSubstitution(t *testing.T) {
	ctx := &config.SubstitutionContext{
		LocalWorkspaceFolder:     "/home/user/project",
		ContainerWorkspaceFolder: "/workspace",
		DevcontainerID:           "abc123",
		ContainerEnv: map[string]string{
			"PATH":  "/usr/bin:/bin",
			"SHELL": "/bin/bash",
		},
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "${localWorkspaceFolder}",
			input:    "${localWorkspaceFolder}",
			expected: "/home/user/project",
		},
		{
			name:     "${localWorkspaceFolderBasename}",
			input:    "${localWorkspaceFolderBasename}",
			expected: "project", // basename of /home/user/project
		},
		{
			name:     "${containerWorkspaceFolder}",
			input:    "${containerWorkspaceFolder}",
			expected: "/workspace",
		},
		{
			name:     "${devcontainerId}",
			input:    "${devcontainerId}",
			expected: "abc123",
		},
		{
			name:     "${localEnv:MISSING_VAR_12345:default}",
			input:    "${localEnv:MISSING_VAR_12345:default}",
			expected: "default",
		},
		{
			name:     "${containerEnv:PATH}",
			input:    "${containerEnv:PATH}",
			expected: "/usr/bin:/bin",
		},
		{
			name:     "${containerEnv:MISSING:fallback}",
			input:    "${containerEnv:MISSING:fallback}",
			expected: "fallback",
		},
		{
			name:     "mixed substitutions",
			input:    "${localWorkspaceFolder}:${containerWorkspaceFolder}",
			expected: "/home/user/project:/workspace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := config.Substitute(tt.input, ctx)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestUserEnvProbe verifies userEnvProbe implementation.
func TestUserEnvProbe(t *testing.T) {
	tests := []struct {
		name        string
		probeType   env.ProbeType
		expectedCmd []string
	}{
		{
			name:        "none",
			probeType:   env.ProbeNone,
			expectedCmd: nil,
		},
		{
			name:        "loginShell",
			probeType:   env.ProbeLoginShell,
			expectedCmd: []string{"sh", "-l", "-c", "env"},
		},
		{
			name:        "loginInteractiveShell",
			probeType:   env.ProbeLoginInteractiveShell,
			expectedCmd: []string{"sh", "-l", "-i", "-c", "env"},
		},
		{
			name:        "interactiveShell",
			probeType:   env.ProbeInteractiveShell,
			expectedCmd: []string{"sh", "-i", "-c", "env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := env.ProbeCommand(tt.probeType)
			assert.Equal(t, tt.expectedCmd, cmd)
		})
	}
}

// TestParseProbeType verifies probe type parsing.
func TestParseProbeType(t *testing.T) {
	tests := []struct {
		input    string
		expected env.ProbeType
	}{
		{"none", env.ProbeNone},
		{"", env.ProbeNone},
		{"loginShell", env.ProbeLoginShell},
		{"loginInteractiveShell", env.ProbeLoginInteractiveShell},
		{"interactiveShell", env.ProbeInteractiveShell},
		{"invalid", env.ProbeNone},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := env.ParseProbeType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestImageMetadataMerging verifies image metadata merging per spec.
func TestImageMetadataMerging(t *testing.T) {
	t.Run("parse valid metadata", func(t *testing.T) {
		label := `[{"remoteUser": "vscode"}, {"capAdd": ["SYS_PTRACE"]}]`
		configs, err := config.ParseImageMetadata(label)
		require.NoError(t, err)
		require.Len(t, configs, 2)
		assert.Equal(t, "vscode", configs[0].RemoteUser)
		assert.Equal(t, []string{"SYS_PTRACE"}, configs[1].CapAdd)
	})

	t.Run("boolean merging - true wins", func(t *testing.T) {
		val := true
		local := &config.DevcontainerConfig{}
		image := []config.DevcontainerConfig{{Init: &val}}
		merged := config.MergeMetadata(local, image)
		require.NotNil(t, merged.Init)
		assert.True(t, *merged.Init)
	})

	t.Run("array merging - union", func(t *testing.T) {
		local := &config.DevcontainerConfig{
			CapAdd: []string{"SYS_PTRACE"},
		}
		image := []config.DevcontainerConfig{{
			CapAdd: []string{"NET_ADMIN"},
		}}
		merged := config.MergeMetadata(local, image)
		assert.ElementsMatch(t, []string{"SYS_PTRACE", "NET_ADMIN"}, merged.CapAdd)
	})

	t.Run("single value - local wins", func(t *testing.T) {
		local := &config.DevcontainerConfig{RemoteUser: "local"}
		image := []config.DevcontainerConfig{{RemoteUser: "image"}}
		merged := config.MergeMetadata(local, image)
		assert.Equal(t, "local", merged.RemoteUser)
	})

	t.Run("image fills missing local values", func(t *testing.T) {
		local := &config.DevcontainerConfig{}
		image := []config.DevcontainerConfig{{RemoteUser: "image", WorkspaceFolder: "/workspace"}}
		merged := config.MergeMetadata(local, image)
		assert.Equal(t, "image", merged.RemoteUser)
		assert.Equal(t, "/workspace", merged.WorkspaceFolder)
	})
}

// TestMountFormats verifies both string and object mount formats.
func TestMountFormats(t *testing.T) {
	t.Run("string format parsing", func(t *testing.T) {
		input := `"type=bind,source=/src,target=/dst,readonly"`
		var m config.Mount
		err := json.Unmarshal([]byte(input), &m)
		require.NoError(t, err)

		assert.Equal(t, "/src", m.Source)
		assert.Equal(t, "/dst", m.Target)
		assert.Equal(t, "bind", m.Type)
		assert.True(t, m.ReadOnly)
	})

	t.Run("object format parsing", func(t *testing.T) {
		input := `{"source": "/src", "target": "/dst", "type": "volume", "readonly": true}`
		var m config.Mount
		err := json.Unmarshal([]byte(input), &m)
		require.NoError(t, err)

		assert.Equal(t, "/src", m.Source)
		assert.Equal(t, "/dst", m.Target)
		assert.Equal(t, "volume", m.Type)
		assert.True(t, m.ReadOnly)
	})

	t.Run("mount string output", func(t *testing.T) {
		m := config.Mount{
			Source: "/src",
			Target: "/dst",
			Type:   "bind",
		}
		assert.Equal(t, "type=bind,source=/src,target=/dst", m.String())

		m.ReadOnly = true
		assert.Equal(t, "type=bind,source=/src,target=/dst,readonly", m.String())
	})
}

// TestComposeDetection verifies compose vs single container detection.
func TestComposeDetection(t *testing.T) {
	t.Run("compose plan detected", func(t *testing.T) {
		input := `{"dockerComposeFile": "docker-compose.yml", "service": "app"}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.True(t, cfg.IsComposePlan())
		assert.False(t, cfg.IsSinglePlan())
	})

	t.Run("image plan detected", func(t *testing.T) {
		input := `{"image": "ubuntu:22.04"}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.False(t, cfg.IsComposePlan())
		assert.True(t, cfg.IsSinglePlan())
	})

	t.Run("build plan detected", func(t *testing.T) {
		input := `{"build": {"dockerfile": "Dockerfile"}}`
		var cfg config.DevcontainerConfig
		err := json.Unmarshal([]byte(input), &cfg)
		require.NoError(t, err)

		assert.False(t, cfg.IsComposePlan())
		assert.True(t, cfg.IsSinglePlan())
	})
}
