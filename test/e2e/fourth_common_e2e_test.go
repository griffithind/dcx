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

// TestFeaturesOnNonRootImageE2E guards features installing as the image's
// USER (F1): on a base ending in a non-root USER every install.sh ran
// unprivileged and failed. The image's user must be restored afterwards.
func TestFeaturesOnNonRootImageE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".devcontainer")
	featureDir := filepath.Join(dir, "features", "needs-root")
	require.NoError(t, os.MkdirAll(featureDir, 0o755))
	write := func(path, content string, mode os.FileMode) {
		require.NoError(t, os.WriteFile(path, []byte(content), mode))
	}
	write(filepath.Join(dir, "Dockerfile"), "FROM alpine:3.24\nRUN adduser -D app\nUSER app\n", 0o644)
	write(filepath.Join(featureDir, "devcontainer-feature.json"), `{"id": "needs-root", "version": "1.0.0", "name": "Needs Root"}`, 0o644)
	write(filepath.Join(featureDir, "install.sh"), "#!/bin/sh\nset -e\nid -u > /usr/local/share/installed-as\n", 0o755)
	write(filepath.Join(dir, "devcontainer.json"), fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"remoteUser": "app",
		"updateRemoteUserUID": false,
		"features": {"./features/needs-root": {}}
	}`, helpers.UniqueTestName(t)), 0o644)
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down") })

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/usr/local/share/installed-as")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, "0", strings.TrimSpace(stdout), "the feature installed as root")

	out, err := exec.Command("docker", "inspect", "--format", "{{.Config.User}}", helpers.ContainerName(t, workspace)).Output()
	require.NoError(t, err)
	assert.Equal(t, "app", strings.TrimSpace(string(out)), "the image's USER is restored")
}
