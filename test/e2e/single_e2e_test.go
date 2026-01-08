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

// TestSingleImageBasedE2E tests the full lifecycle of an image-based devcontainer.
func TestSingleImageBasedE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create a temp workspace with image-based config
	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

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
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "hello-from-single")
		require.NoError(t, err)
		assert.Contains(t, stdout, "hello-from-single")
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

// TestSingleDockerfileBasedE2E tests devcontainer with Dockerfile.
func TestSingleDockerfileBasedE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create temp workspace with Dockerfile config
	tmpDir := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create Dockerfile
	dockerfile := `FROM alpine:latest
RUN echo "built from dockerfile" > /built-marker
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "Dockerfile"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	// Create devcontainer.json
	devcontainerJSON := `{
		"name": "Dockerfile Test",
		"build": {
			"dockerfile": "Dockerfile"
		},
		"workspaceFolder": "/workspace"
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	// Setup cleanup
	t.Cleanup(func() {
		helpers.RunDCXInDir(t, tmpDir, "down")
	})

	// Test dcx up with Dockerfile
	t.Run("up_builds_and_runs", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, tmpDir, "up")
		assert.Contains(t, stdout, "Environment is ready")

		state := helpers.GetContainerState(t, tmpDir)
		assert.Equal(t, "RUNNING", state)
	})

	// Test that built content exists
	t.Run("dockerfile_executed", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/built-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "built from dockerfile")
	})

	// Clean up
	t.Run("down_removes_container", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, tmpDir, "down")
		assert.Contains(t, stdout, "removed")
	})
}

// TestSingleContainerWithEnv tests environment variables.
func TestSingleContainerWithEnv(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Single Container Env Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"containerEnv": {
			"MY_TEST_VAR": "test-value-123"
		}
	}`

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test env var is set
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "MY_TEST_VAR")
	require.NoError(t, err)
	assert.Contains(t, stdout, "test-value-123")
}

// TestSingleContainerWithMounts tests volume mounts.
func TestSingleContainerWithMounts(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Mount Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace"
	}`

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Create a test file in the workspace
	testFile := filepath.Join(workspace, "test-file.txt")
	err := os.WriteFile(testFile, []byte("mounted content"), 0644)
	require.NoError(t, err)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test workspace mount
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/workspace/test-file.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "mounted content")
}

// TestSingleContainerRebuild tests the --rebuild flag.
func TestSingleContainerRebuild(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create temp workspace with Dockerfile
	tmpDir := t.TempDir()

	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create Dockerfile
	dockerfile := `FROM alpine:latest
RUN echo "version1" > /version
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "Dockerfile"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	// Create devcontainer.json
	devcontainerJSON := `{
		"name": "Rebuild Test",
		"build": {
			"dockerfile": "Dockerfile"
		},
		"workspaceFolder": "/workspace"
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, tmpDir, "down")
	})

	// First up
	helpers.RunDCXInDirSuccess(t, tmpDir, "up")

	// Verify version1
	stdout, _, err := helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/version")
	require.NoError(t, err)
	assert.Contains(t, stdout, "version1")

	// Modify Dockerfile
	dockerfile = `FROM alpine:latest
RUN echo "version2" > /version
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "Dockerfile"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	// Rebuild
	helpers.RunDCXInDirSuccess(t, tmpDir, "up", "--rebuild")

	// Verify version2
	stdout, _, err = helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/version")
	require.NoError(t, err)
	assert.Contains(t, stdout, "version2")
}

// TestSingleContainerLabelsE2E tests that all required labels are set on containers.
func TestSingleContainerLabelsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

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
			`{{index .Config.Labels "com.griffithind.dcx.workspace.path"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, workspace, labelValue,
			"workspace_path label should match the workspace directory")
	})

	// Test workspace_id label is set
	t.Run("workspace_id_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.workspace.id"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.NotEmpty(t, labelValue, "workspace_id label should be set")
		assert.Len(t, labelValue, 12, "workspace_id should be 12 characters")
	})

	// Test managed label is set
	t.Run("managed_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.managed"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, "true", labelValue, "managed label should be true")
	})

	// Test build_method label is set to image
	t.Run("build_method_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.build.method"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, "image", labelValue, "build_method label should be image")
	})

	// Test primary label is set
	t.Run("primary_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.container.primary"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, "true", labelValue, "primary label should be true")
	})

	// Test config_hash label is set
	t.Run("config_hash_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.hash.config"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.NotEmpty(t, labelValue, "config_hash label should be set")
	})

	// Test overall_hash label is set
	t.Run("overall_hash_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.hash.overall"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.NotEmpty(t, labelValue, "overall_hash label should be set")
	})
}
