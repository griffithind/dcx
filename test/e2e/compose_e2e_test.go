//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestComposeWorkflowE2E tests the full lifecycle of a compose-based devcontainer.
func TestComposeWorkflowE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	// Create a temp workspace with compose config
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`, helpers.UniqueTestName(t))

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:3.24
    command: sleep infinity
    init: true
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	// Test initial state is absent
	t.Run("initial_state_absent", func(t *testing.T) {
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "absent", state)
	})

	// Test dcx up
	t.Run("up_creates_running_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Devcontainer started successfully")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "running", state)
	})

	// Test dcx ssh -- <cmd>
	t.Run("exec_runs_command", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "hello")
		require.NoError(t, err)
		assert.Contains(t, stdout, "hello")
	})

	// Test dcx stop
	t.Run("stop_stops_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Contains(t, stdout, "stopped")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "created", state)
	})

	// Test dcx up (starts stopped container)
	t.Run("up_starts_stopped_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "started")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "running", state)
	})

	// Test dcx down
	t.Run("down_removes_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Contains(t, stdout, "removed")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "absent", state)
	})
}

// TestComposeMultiServiceE2E: a project whose primary service depends on
// another starts both and takes commands; a second up changes nothing.
func TestComposeMultiServiceE2E(t *testing.T) {
	t.Parallel()
	env := composeEnv(t)
	assert.Contains(t, env.UpOutput, "Devcontainer started successfully")
	assert.Equal(t, "running", helpers.GetContainerState(t, env.Dir))
	stdout, _, err := helpers.RunDCXInDir(t, env.Dir, "ssh", "--", "echo", "test")
	require.NoError(t, err)
	assert.Contains(t, stdout, "test")

	assert.Contains(t, helpers.RunDCXInDirSuccess(t, env.Dir, "up"), "already running", "up is idempotent")
	assert.Equal(t, "running", helpers.GetContainerState(t, env.Dir))
}

// TestComposeRecreate tests the --recreate flag.
func TestComposeRecreate(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`, helpers.UniqueTestName(t))

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:3.24
    command: sleep infinity
    init: true
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	// First up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	before := helpers.GetStatus(t, workspace).Container
	require.NotNil(t, before)

	helpers.RunDCXInDirSuccess(t, workspace, "up", "--recreate")

	after := helpers.GetStatus(t, workspace).Container
	require.NotNil(t, after)
	assert.NotEqual(t, before.ID, after.ID, "--recreate must replace the container")
}

// TestComposeContainerLabelsE2E: the primary compose container carries
// dcx's labels.
func TestComposeContainerLabelsE2E(t *testing.T) {
	t.Parallel()
	env := composeEnv(t)
	containerName := helpers.ContainerName(t, env.Dir)
	label := func(name string) string {
		out, err := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.`+name+`"}}`, containerName).CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", out)
		return strings.TrimSpace(string(out))
	}
	assert.Equal(t, env.Dir, label("workspace.path"))
	assert.Equal(t, "compose", label("build.method"))
	assert.NotEmpty(t, label("compose.project"))
	assert.Equal(t, "app", label("compose.service"))
	assert.Equal(t, "true", label("container.primary"))
	assert.Equal(t, "true", label("managed"))
}

// TestComposeLocalFeatureCachingE2E tests feature caching with local features.
// This consolidates tests for: basic caching, down/up cycle caching, project name, and dockerfile.
func TestComposeLocalFeatureCachingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	workspace := createComposeWorkspaceWithFeatures(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	containerID := helpers.ContainerID(t, workspace)
	imageID := helpers.ContainerImageID(t, workspace)

	t.Run("second_up_keeps_the_container", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Equal(t, containerID, helpers.ContainerID(t, workspace))
	})

	t.Run("feature_active", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/feature-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "feature installed")
	})

	t.Run("up_after_down_reuses_the_features_image", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Equal(t, "absent", helpers.GetContainerState(t, workspace))

		helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.NotEqual(t, containerID, helpers.ContainerID(t, workspace), "a new container")
		assert.Equal(t, imageID, helpers.ContainerImageID(t, workspace), "from the cached image")

		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/feature-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "feature installed")
	})
}

// TestComposeRemoteFeatureCachingE2E tests feature caching with a remote OCI feature.
func TestComposeRemoteFeatureCachingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	workspace := createComposeWorkspaceWithRemoteFeature(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	containerID := helpers.ContainerID(t, workspace)

	// A second up changes nothing: same container, no rebuild.
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Contains(t, stdout, "already running")
	assert.Equal(t, containerID, helpers.ContainerID(t, workspace))
}

// createComposeWorkspaceWithRemoteFeature creates a workspace with a remote feature.
func createComposeWorkspaceWithRemoteFeature(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create devcontainer.json with compose and a remote feature
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"features": {
			"ghcr.io/devcontainers/features/common-utils:2": {
				"installZsh": false,
				"installOhMyZsh": false,
				"configureZshAsDefaultShell": false,
				"upgradePackages": false
			}
		}
	}`, helpers.UniqueTestName(t))
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	// Create docker-compose.yml
	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: debian:bookworm-slim
    command: sleep infinity
    init: true
    volumes:
      - ..:/workspace:cached
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "docker-compose.yml"), []byte(dockerComposeYAML), 0644)
	require.NoError(t, err)

	return tmpDir
}

// createComposeWorkspaceWithFeatures creates a workspace with compose config and features.
func createComposeWorkspaceWithFeatures(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create feature directory
	featureDir := filepath.Join(devcontainerDir, "features", "simple-marker")
	err = os.MkdirAll(featureDir, 0755)
	require.NoError(t, err)

	// Create feature metadata
	featureJSON := `{
		"id": "simple-marker",
		"version": "1.0.0",
		"name": "Simple Marker Feature",
		"description": "Creates a marker file"
	}`
	err = os.WriteFile(filepath.Join(featureDir, "devcontainer-feature.json"), []byte(featureJSON), 0644)
	require.NoError(t, err)

	// Create install script
	installScript := `#!/bin/sh
set -e
echo "feature installed" > /tmp/feature-marker
`
	err = os.WriteFile(filepath.Join(featureDir, "install.sh"), []byte(installScript), 0755)
	require.NoError(t, err)

	// Create devcontainer.json with compose and features
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/simple-marker": {}
		}
	}`, helpers.UniqueTestName(t))
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	// Create docker-compose.yml
	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:3.24
    command: sleep infinity
    init: true
    volumes:
      - ..:/workspace:cached
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "docker-compose.yml"), []byte(dockerComposeYAML), 0644)
	require.NoError(t, err)

	return tmpDir
}
