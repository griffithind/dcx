//go:build e2e

package e2e

import (
	"os/exec"
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
