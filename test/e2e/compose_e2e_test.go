//go:build e2e

package e2e

import (
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
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	// Create a temp workspace with compose config
	devcontainerJSON := `{
		"name": "E2E Compose Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

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

	// Test dcx start
	t.Run("start_starts_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "start")
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
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "Multi-Service Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"runServices": ["app", "db"],
		"workspaceFolder": "/workspace"
	}`

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
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "Idempotent Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

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
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "Recreate Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

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
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "Labels Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

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

	// Test workspace_path label is set correctly
	t.Run("workspace_path_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "io.github.dcx.workspace_path"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, workspace, labelValue,
			"workspace_path label should match the workspace directory")
	})

	// Test plan label is set to compose
	t.Run("plan_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "io.github.dcx.plan"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, "compose", labelValue, "plan label should be compose")
	})

	// Test compose_project label is set
	t.Run("compose_project_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "io.github.dcx.compose_project"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.NotEmpty(t, labelValue, "compose_project label should be set")
	})

	// Test primary_service label is set
	t.Run("primary_service_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "io.github.dcx.primary_service"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, "app", labelValue, "primary_service label should be app")
	})

	// Test primary label is set
	t.Run("primary_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "io.github.dcx.primary"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, "true", labelValue, "primary label should be true")
	})

	// Test managed label is set
	t.Run("managed_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "io.github.dcx.managed"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, "true", labelValue, "managed label should be true")
	})
}

// TestComposeWithFeaturesCachingE2E tests that features are cached between runs for compose environments.
func TestComposeWithFeaturesCachingE2E(t *testing.T) {
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
		// Should see feature building output
		assert.Contains(t, stdout, "Building derived image")
	})

	// Second up - features should be cached (no "Building derived image" message expected
	// since the container is already running and config hasn't changed)
	t.Run("second_up_uses_cache", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		// Should see that environment is already running
		assert.Contains(t, stdout, "already running")
		// Should NOT see feature building output since container is running
		assert.NotContains(t, stdout, "Building derived image")
	})

	// Verify feature is still active
	t.Run("feature_still_active", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/feature-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "feature installed")
	})
}

// TestComposeWithFeaturesAndProjectNameE2E tests feature caching with a dcx.json project name.
func TestComposeWithFeaturesAndProjectNameE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	workspace := createComposeWorkspaceWithFeaturesAndProjectName(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// First up - features should be installed
	t.Run("first_up_installs_features", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")
		// Should see feature building output
		assert.Contains(t, stdout, "Building derived image")
	})

	// Second up - features should be cached
	t.Run("second_up_uses_cache", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		// Should see that environment is already running
		assert.Contains(t, stdout, "already running")
		// Should NOT see feature building output since container is running
		assert.NotContains(t, stdout, "Building derived image")
	})
}

// createComposeWorkspaceWithFeaturesAndProjectName creates a workspace with project name.
func createComposeWorkspaceWithFeaturesAndProjectName(t *testing.T) string {
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
	devcontainerJSON := `{
		"name": "Compose Features Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/simple-marker": {}
		}
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	// Create dcx.json with project name
	dcxJSON := `{
		"name": "my-test-project"
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "dcx.json"), []byte(dcxJSON), 0644)
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

// TestComposeWithFeaturesDownUpCycleE2E tests that features are cached across down/up cycles.
func TestComposeWithFeaturesDownUpCycleE2E(t *testing.T) {
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

	// Down the environment
	t.Run("down_removes_containers", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Contains(t, stdout, "removed")
	})

	// Up again - should use cached features image
	t.Run("second_up_uses_cached_features", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")
		// Should use cached derived image, NOT rebuild features
		assert.Contains(t, stdout, "Using cached derived image")
		assert.NotContains(t, stdout, "Building derived image")
	})

	// Verify feature is still active
	t.Run("feature_still_active", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/feature-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "feature installed")
	})
}

// TestComposeWithRemoteFeatureCachingE2E tests feature caching with a remote feature.
func TestComposeWithRemoteFeatureCachingE2E(t *testing.T) {
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
		// Should see feature building output
		assert.Contains(t, stdout, "Building derived image")
	})

	// Second up - should return early, not rebuild
	t.Run("second_up_uses_cache", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		// Should see that environment is already running
		assert.Contains(t, stdout, "already running")
		// Should NOT see feature building output since container is running
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
	devcontainerJSON := `{
		"name": "Compose Remote Feature Test",
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
	}`
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

// TestComposeWithFeaturesAndDockerfileE2E tests feature caching with a service that has a Dockerfile.
func TestComposeWithFeaturesAndDockerfileE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	workspace := createComposeWorkspaceWithFeaturesAndDockerfile(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// First up - features should be installed
	t.Run("first_up_installs_features", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")
		// Should see feature building output
		assert.Contains(t, stdout, "Building derived image")
	})

	// Second up - should return early, not rebuild
	t.Run("second_up_uses_cache", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		// Should see that environment is already running
		assert.Contains(t, stdout, "already running")
		// Should NOT see feature building output since container is running
		assert.NotContains(t, stdout, "Building derived image")
		// Should NOT see compose building output
		assert.NotContains(t, stdout, "Building 1 service")
	})
}

// createComposeWorkspaceWithFeaturesAndDockerfile creates a workspace with a service that has a Dockerfile.
func createComposeWorkspaceWithFeaturesAndDockerfile(t *testing.T) string {
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

	// Create Dockerfile for the app service
	dockerfile := `FROM alpine:latest
RUN echo "built from dockerfile"
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "Dockerfile"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	// Create devcontainer.json with compose and features
	devcontainerJSON := `{
		"name": "Compose Features Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/simple-marker": {}
		}
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	// Create docker-compose.yml with build context
	dockerComposeYAML := `version: '3.8'
services:
  app:
    build:
      context: .
      dockerfile: Dockerfile
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
	devcontainerJSON := `{
		"name": "Compose Features Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/simple-marker": {}
		}
	}`
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
