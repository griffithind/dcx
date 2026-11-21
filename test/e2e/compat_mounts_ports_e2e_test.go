//go:build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGitRootIsMountedE2E: a workspace in a subfolder of a git repository
// mounts the repository (C2), so .git and sibling folders are there, and
// the working directory is the subfolder inside it.
func TestGitRootIsMountedE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	repo := helpers.CreateWorkspaceFiles(t, map[string]string{
		".git/HEAD":      "ref: refs/heads/main\n",
		"shared/VERSION": "1.2.3\n",
		"services/api/.devcontainer/devcontainer.json": fmt.Sprintf(`{"name": %q, "image": "alpine:3.24"}`,
			helpers.UniqueTestName(t)),
	})
	ws := filepath.Join(repo, "services", "api")
	t.Cleanup(func() { helpers.RunDCXInDir(t, ws, "down") })

	helpers.RunDCXInDirSuccess(t, ws, "up")

	stdout, stderr, err := helpers.RunDCXInDir(t, ws, "ssh", "--", "sh", "-c", "pwd && cat ../../shared/VERSION && test -d ../../.git && echo has-git")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	lines := strings.Fields(stdout)
	require.Len(t, lines, 3, stdout)
	assert.Equal(t, "/workspaces/"+filepath.Base(repo)+"/services/api", lines[0])
	assert.Equal(t, "1.2.3", lines[1])
	assert.Equal(t, "has-git", lines[2])
}

// TestComposePublishedForwardPortE2E: a port the compose file publishes
// and forwardPorts also lists (composeEnv's) is published once (R1); it
// used to fail with "address already in use".
func TestComposePublishedForwardPortE2E(t *testing.T) {
	t.Parallel()
	ws := composeEnv(t).Dir
	assert.Equal(t, "running", helpers.GetContainerState(t, ws))
	out, err := exec.Command("docker", "port", helpers.ContainerName(t, ws)).CombinedOutput()
	require.NoError(t, err, string(out))
	assert.Equal(t, 1, strings.Count(string(out), fmt.Sprintf("127.0.0.1:%d", composePort)), string(out))
}

// TestAppPortRangeE2E: an appPort range (richEnv's) publishes every port in
// it (C17).
func TestAppPortRangeE2E(t *testing.T) {
	t.Parallel()
	out, err := exec.Command("docker", "port", helpers.ContainerName(t, richEnv(t).Dir)).CombinedOutput()
	require.NoError(t, err, string(out))
	for _, port := range []string{"8000/tcp", "8001/tcp", "8002/tcp"} {
		assert.Contains(t, string(out), port)
	}
}
