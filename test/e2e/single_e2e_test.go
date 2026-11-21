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

// TestDockerfileLifecycleE2E walks a Dockerfile config through dcx build
// (with and without the cache), up (the Dockerfile ran), clean (an image
// in use is kept), an edit applied with up --rebuild, down, and clean (the
// image no container uses is removed). clean --current touches only this
// workspace's images, so the test runs in parallel. Build secrets are
// available to RUN --mount and nowhere else; a runtime secret is mounted
// alongside.
func TestDockerfileLifecycleE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"customizations": {"dcx": {
			"secrets": {"RUNTIME_SECRET": "echo runtime-value"},
			"buildSecrets": {
				"BUILD_ONLY": "echo build-time-only",
				"SENSITIVE": "echo super-secret-do-not-persist"
			}
		}}
	}`, helpers.UniqueTestName(t)))
	dockerfile := filepath.Join(ws, ".devcontainer", "Dockerfile")
	writeVersion := func(v string) {
		require.NoError(t, os.WriteFile(dockerfile, []byte(`FROM alpine:3.24
RUN --mount=type=secret,id=BUILD_ONLY cat /run/secrets/BUILD_ONLY > /build-time-value
RUN --mount=type=secret,id=SENSITIVE echo "secret was accessed" > /build-status
RUN echo "`+v+`" > /version
`), 0o644))
	}
	writeVersion("version1")

	assert.Contains(t, helpers.RunDCXInDirSuccess(t, ws, "build"), "Fetching build secrets")
	assert.NotEmpty(t, helpers.RunDCXInDirSuccess(t, ws, "build", "--no-cache"), "no-cache build should produce output")

	up := helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Contains(t, up, "Devcontainer started successfully")
	assert.Contains(t, up, "Fetching runtime secrets")
	assert.Equal(t, "running", helpers.GetContainerState(t, ws))
	assert.Equal(t, "version1", dcxExec(t, ws, "cat", "/version"), "the Dockerfile ran")
	assert.Equal(t, "build-time-only", dcxExec(t, ws, "cat", "/build-time-value"))
	assert.Equal(t, "secret was accessed", dcxExec(t, ws, "cat", "/build-status"))
	assert.Equal(t, "runtime-value", dcxExec(t, ws, "cat", "/run/secrets/RUNTIME_SECRET"))
	for _, name := range []string{"BUILD_ONLY", "SENSITIVE"} {
		_, _, err := helpers.RunDCXInDir(t, ws, "ssh", "--", "test", "-e", "/run/secrets/"+name)
		assert.Error(t, err, "build secret %s must not exist at runtime", name)
	}

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
