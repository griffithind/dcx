//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dcxExec(t *testing.T, workspace string, args ...string) string {
	t.Helper()
	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, append([]string{"ssh", "--"}, args...)...)
	require.NoError(t, err, "exec %v: stdout=%s stderr=%s", args, stdout, stderr)
	return strings.TrimSpace(stdout)
}

// runArgs beyond the few dcx parses (hostname, env, labels, ulimits) must
// reach `docker run`, and --rm must not make the container vanish on stop.
func TestRunArgsPassThroughE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"runArgs": ["--hostname=dcx-runargs", "-e", "FROM_RUNARGS=yes", "--label", "dcx.test.runargs=1", "--ulimit", "nofile=1024:2048", "--rm"]
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down") })

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Equal(t, "dcx-runargs", dcxExec(t, workspace, "hostname"))
	assert.Equal(t, "yes", dcxExec(t, workspace, "printenv", "FROM_RUNARGS"))
	assert.Equal(t, "1024", dcxExec(t, workspace, "sh", "-c", "ulimit -n"))

	// --rm was ignored: the container survives a stop.
	helpers.RunDCXInDirSuccess(t, workspace, "stop")
	assert.Equal(t, "created", helpers.GetContainerState(t, workspace))
}

// A config passed with --config must get the same substitution as an
// auto-discovered one, and ${containerEnv:VAR} in remoteEnv must resolve
// against the running container.
func TestExplicitConfigAndContainerEnvRefsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := t.TempDir()
	variant := filepath.Join(workspace, ".devcontainer", "variant")
	require.NoError(t, os.MkdirAll(variant, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(variant, "devcontainer.json"), []byte(fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:${localEnv:DCX_E2E_UNSET_TAG:latest}",
		"workspaceFolder": "/src/${localWorkspaceFolderBasename}",
		"workspaceMount": "type=bind,source=${localWorkspaceFolder},target=/src/${localWorkspaceFolderBasename}",
		"remoteEnv": {"PATH": "${containerEnv:PATH}:/custom/bin"}
	}`, helpers.UniqueTestName(t))), 0o644))
	cfg := ".devcontainer/variant/devcontainer.json"
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "-c", cfg, "down") })

	helpers.RunDCXInDirSuccess(t, workspace, "-c", cfg, "up")

	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "-c", cfg, "ssh", "--", "sh", "-c", "pwd; echo $PATH")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.Len(t, lines, 2, stdout)
	assert.Equal(t, "/src/"+filepath.Base(workspace), lines[0])
	assert.True(t, strings.HasSuffix(lines[1], ":/custom/bin"), "PATH = %q", lines[1])
	assert.Contains(t, lines[1], "/usr/bin")
	assert.NotContains(t, lines[1], "containerEnv")
}
