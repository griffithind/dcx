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
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	// Setup cleanup
	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Test initial state is ABSENT
	t.Run("initial_state_absent", func(t *testing.T) {
		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "ABSENT", state)
	})

	// Test dcx up
	t.Run("up_creates_running_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// Test dcx exec
	t.Run("exec_runs_command", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "hello")
		require.NoError(t, err)
		assert.Contains(t, stdout, "hello")
	})

	// Test dcx stop
	t.Run("stop_stops_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Contains(t, stdout, "stopped")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "CREATED", state)
	})

	// Test dcx up (starts stopped container)
	t.Run("up_starts_stopped_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "started")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// Test dcx down
	t.Run("down_removes_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Contains(t, stdout, "removed")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "ABSENT", state)
	})
}

// TestComposeMultiServiceE2E tests compose with multiple services.
func TestComposeMultiServiceE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"runServices": ["app", "db"],
		"workspaceFolder": "/workspace"
	}`, helpers.UniqueTestName(t))

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
    depends_on:
      - db
  db:
    image: alpine:latest
    command: sleep infinity
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the environment
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Contains(t, stdout, "Environment is ready")

	// Verify state
	state := helpers.GetContainerState(t, workspace)
	assert.Equal(t, "RUNNING", state)

	// Verify we can exec into the primary service
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "test")
	require.NoError(t, err)
	assert.Contains(t, stdout, "test")
}

// TestComposeUpIdempotent tests that dcx up is idempotent.
func TestComposeUpIdempotent(t *testing.T) {
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
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// First up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Second up should succeed without recreating
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Contains(t, stdout, "already running")

	// State should still be RUNNING
	state := helpers.GetContainerState(t, workspace)
	assert.Equal(t, "RUNNING", state)
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
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// First up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Get container info before recreate
	statusBefore := helpers.RunDCXInDirSuccess(t, workspace, "status")

	// Recreate
	helpers.RunDCXInDirSuccess(t, workspace, "up", "--recreate")

	// Get container info after recreate
	statusAfter := helpers.RunDCXInDirSuccess(t, workspace, "status")

	// Container IDs should be different (we can check the status output changed)
	// The ID line will be different after recreate
	assert.NotEqual(t, extractContainerID(statusBefore), extractContainerID(statusAfter))
}

// extractContainerID extracts the container ID from status output.
func extractContainerID(status string) string {
	for _, line := range strings.Split(status, "\n") {
		if strings.Contains(line, "ID:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

// TestComposeContainerLabelsE2E tests that all required labels are set on compose containers.
func TestComposeContainerLabelsE2E(t *testing.T) {
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
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Get container name from status
	statusOut := helpers.RunDCXInDirSuccess(t, workspace, "status")
	var containerName string
	for _, line := range strings.Split(statusOut, "\n") {
		if strings.Contains(line, "Name:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				containerName = parts[1]
			}
		}
	}
	require.NotEmpty(t, containerName, "should find container name")

	// Test all labels in a single subtest group
	t.Run("labels", func(t *testing.T) {
		// workspace_path label
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.workspace.path"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)
		assert.Equal(t, workspace, strings.TrimSpace(string(output)))

		// build_method label
		cmd = exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.build.method"}}`,
			containerName)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err)
		assert.Equal(t, "compose", strings.TrimSpace(string(output)))

		// compose_project label
		cmd = exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.compose.project"}}`,
			containerName)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err)
		assert.NotEmpty(t, strings.TrimSpace(string(output)))

		// compose_service label
		cmd = exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.compose.service"}}`,
			containerName)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err)
		assert.Equal(t, "app", strings.TrimSpace(string(output)))

		// primary label
		cmd = exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.container.primary"}}`,
			containerName)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err)
		assert.Equal(t, "true", strings.TrimSpace(string(output)))

		// managed label
		cmd = exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.managed"}}`,
			containerName)
		output, err = cmd.CombinedOutput()
		require.NoError(t, err)
		assert.Equal(t, "true", strings.TrimSpace(string(output)))
	})
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

	// First up - features should be installed
	t.Run("first_up_installs_features", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")
		assert.Contains(t, stdout, "Building derived image")
	})

	// Second up - should be cached (container already running)
	t.Run("second_up_uses_running_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "already running")
		assert.NotContains(t, stdout, "Building derived image")
	})

	// Verify feature is active
	t.Run("feature_active", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/feature-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "feature installed")
	})

	// Down the environment
	t.Run("down_removes_containers", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Contains(t, stdout, "removed")
	})

	// Up again - should use cached features image
	t.Run("up_after_down_uses_cached_image", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")
		assert.Contains(t, stdout, "Using cached derived image")
		assert.NotContains(t, stdout, "Building derived image")
	})

	// Verify feature is still active after down/up cycle
	t.Run("feature_still_active_after_cycle", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/feature-marker")
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

	// First up - features should be installed
	t.Run("first_up_installs_features", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")
		assert.Contains(t, stdout, "Building derived image")
	})

	// Second up - should return early, not rebuild
	t.Run("second_up_uses_cache", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "already running")
		assert.NotContains(t, stdout, "Building derived image")
	})
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
    image: ubuntu:22.04
    command: sleep infinity
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
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "docker-compose.yml"), []byte(dockerComposeYAML), 0644)
	require.NoError(t, err)

	return tmpDir
}
