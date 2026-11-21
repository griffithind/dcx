//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRemoteEnvE2E: remoteEnv reaches sessions; with containerEnv, remoteEnv
// wins for a variable both set.
func TestRemoteEnvE2E(t *testing.T) {
	t.Parallel()
	workspace := richEnv(t).Dir
	for name, want := range map[string]string{
		"REMOTE_VAR_1":  "remote-value-1",
		"REMOTE_VAR_2":  "remote-value-2",
		"EDITOR":        "vim",
		"REMOTE_VAR":    "remote-value",
		"CONTAINER_VAR": "container-value",
		"SHARED_VAR":    "from-remote",
	} {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "printenv", name)
		require.NoError(t, err, name)
		assert.Contains(t, stdout, want, name)
	}
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "sh", "-c", "printenv REMOTE_VAR_1 && printenv REMOTE_VAR_2")
	require.NoError(t, err)
	assert.Contains(t, stdout, "remote-value-1")
	assert.Contains(t, stdout, "remote-value-2")
}

// TestOverrideCommandDefaultE2E: without overrideCommand (true by default
// for image configs) an image without a long-running command (alpine)
// keeps running on dcx's keep-alive, takes commands, and up waits for the
// create hooks (waitFor has no effect on a command-line up).
func TestOverrideCommandDefaultE2E(t *testing.T) {
	t.Parallel()
	env := richEnv(t)
	for _, want := range []string{"onCreate executed", "updateContent executed", "postCreate executed"} {
		assert.Contains(t, env.UpOutput, want, "up waits for every create hook")
	}
	assert.Contains(t, env.UpOutput, "Devcontainer started successfully")
	assert.Equal(t, "running", helpers.GetContainerState(t, env.Dir))

	out, _, err := helpers.RunDCXInDir(t, env.Dir, "ssh", "--", "echo", "container-is-running")
	require.NoError(t, err)
	assert.Contains(t, out, "container-is-running")
	out, _, err = helpers.RunDCXInDir(t, env.Dir, "ssh", "--", "ps", "aux")
	require.NoError(t, err)
	assert.Contains(t, out, "sleep", "the keep-alive runs")
}

// TestOverrideCommandExplicitFalseE2E tests that setting overrideCommand to false
// does not override the container's command.
func TestOverrideCommandExplicitFalseE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// With overrideCommand=false dcx must not replace the image's command
	// with its keep-alive: PID 1 is the image's own command.
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"overrideCommand": false
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".devcontainer", "Dockerfile"),
		// The command exits on SIGTERM, so stopping the container does not
		// wait out Docker's 10 s timeout.
		[]byte("FROM alpine:3.24\nCMD [\"sh\", \"-c\", \"trap 'exit 0' TERM; tail -f /dev/null & wait\"]\n"), 0o644))

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "sh", "-c", "tr '\\0' ' ' < /proc/1/cmdline")
	require.NoError(t, err)
	assert.Contains(t, stdout, "tail -f /dev/null")
	assert.NotContains(t, stdout, "dcx-keepalive")
}

// TestOverrideCommandFalseWithoutTTYE2E: the container gets no TTY or
// open stdin (R13), as devcontainers/cli creates it, so an image command
// that needs a terminal (alpine's /bin/sh) exits and up says so.
func TestOverrideCommandFalseWithoutTTYE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"overrideCommand": false
	}`, helpers.UniqueTestName(t)))

	_, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
	require.Error(t, err)
	assert.Contains(t, stderr, "not running")
}
