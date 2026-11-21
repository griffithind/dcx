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

// TestSingleImageBasedE2E tests the full lifecycle of an image-based devcontainer.
func TestSingleImageBasedE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create a temp workspace with image-based config
	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:3.24")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

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
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "hello-from-single")
		require.NoError(t, err)
		assert.Contains(t, stdout, "hello-from-single")
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

// TestDockerfileLifecycleE2E walks a Dockerfile config through dcx build
// (with and without the cache), up (the Dockerfile ran), clean (an image
// in use is kept), an edit applied with up --rebuild, down, and clean (the
// image no container uses is removed). clean --current touches only this
// workspace's images, so the test runs in parallel.
func TestDockerfileLifecycleE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace"
	}`, helpers.UniqueTestName(t)))
	dockerfile := filepath.Join(ws, ".devcontainer", "Dockerfile")
	writeVersion := func(v string) {
		require.NoError(t, os.WriteFile(dockerfile, []byte("FROM alpine:3.24\nRUN echo \""+v+"\" > /version\n"), 0o644))
	}
	writeVersion("version1")

	assert.NotEmpty(t, helpers.RunDCXInDirSuccess(t, ws, "build"), "build should produce output")
	assert.NotEmpty(t, helpers.RunDCXInDirSuccess(t, ws, "build", "--no-cache"), "no-cache build should produce output")

	assert.Contains(t, helpers.RunDCXInDirSuccess(t, ws, "up"), "Devcontainer started successfully")
	assert.Equal(t, "running", helpers.GetContainerState(t, ws))
	assert.Equal(t, "version1", dcxExec(t, ws, "cat", "/version"), "the Dockerfile ran")

	image := strings.TrimSpace(func() string {
		out, err := exec.Command("docker", "inspect", "--format", "{{.Config.Image}}", helpers.ContainerID(t, ws)).Output()
		require.NoError(t, err)
		return string(out)
	}())
	require.True(t, strings.HasPrefix(image, "dcx/"), "image %q", image)
	helpers.RunDCXInDirSuccess(t, ws, "clean", "--current")
	require.NoError(t, exec.Command("docker", "image", "inspect", image).Run(), "in-use image must be kept")

	writeVersion("version2")
	helpers.RunDCXInDirSuccess(t, ws, "up", "--rebuild")
	assert.Equal(t, "version2", dcxExec(t, ws, "cat", "/version"), "--rebuild applies the edit")

	rebuilt := strings.TrimSpace(func() string {
		out, err := exec.Command("docker", "inspect", "--format", "{{.Config.Image}}", helpers.ContainerID(t, ws)).Output()
		require.NoError(t, err)
		return string(out)
	}())
	assert.Contains(t, helpers.RunDCXInDirSuccess(t, ws, "down"), "removed")
	helpers.RunDCXInDirSuccess(t, ws, "clean", "--current")
	assert.Error(t, exec.Command("docker", "image", "inspect", rebuilt).Run(), "unused dcx image must be removed")
}

// TestSingleContainerWithMounts tests volume mounts.
func TestSingleContainerWithMounts(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Mount Test",
		"image": "alpine:3.24"
	}`

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Create a test file in the workspace
	testFile := filepath.Join(workspace, "test-file.txt")
	err := os.WriteFile(testFile, []byte("mounted content"), 0644)
	require.NoError(t, err)

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// The default workspace mount: /workspaces/<folder>
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/workspaces/"+filepath.Base(workspace)+"/test-file.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "mounted content")
}

// TestSingleContainerLabelsE2E tests that all required labels are set on containers.
func TestSingleContainerLabelsE2E(t *testing.T) {
	t.Parallel()

	env := alpineEnv(t)
	workspace := env.Dir

	// Get container name from status
	containerName := helpers.ContainerName(t, workspace)

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

}
