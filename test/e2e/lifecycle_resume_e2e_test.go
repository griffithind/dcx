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

// A failed postCreateCommand used to leave a running container that the
// next `dcx up` reported as "already running", never finishing setup. Now
// the next up resumes: completed hooks are not rerun, the failed one is.
func TestFailedCreateHookResumesE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceMount": "type=bind,source=${localWorkspaceFolder},target=/workspace",
		"workspaceFolder": "/workspace",
		"onCreateCommand": "echo ran >> /tmp/oncreate.log",
		"postCreateCommand": "test -f /workspace/ready-to-finish",
		"postStartCommand": "echo ran >> /tmp/poststart.log"
	}`, helpers.UniqueTestName(t)))
	t.Cleanup(func() { helpers.RunDCXInDir(t, ws, "down") })

	_, _, err := helpers.RunDCXInDir(t, ws, "up")
	require.Error(t, err, "postCreateCommand should fail the first time")

	require.NoError(t, os.WriteFile(filepath.Join(ws, "ready-to-finish"), nil, 0o644))
	out := helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Contains(t, out, "resuming")
	assert.NotContains(t, out, "already running")

	assert.Equal(t, "1", dcxExec(t, ws, "sh", "-c", "wc -l < /tmp/oncreate.log | tr -d ' '"), "onCreateCommand must not rerun")
	assert.Equal(t, "1", dcxExec(t, ws, "sh", "-c", "wc -l < /tmp/poststart.log | tr -d ' '"), "postStartCommand runs once setup completes")

	// Now complete: a further up is a no-op.
	assert.Contains(t, helpers.RunDCXInDirSuccess(t, ws, "up"), "already running")
}

// initializeCommand runs on the host before the container is created, so
// it can prepare things the container needs at creation time (here: a bind
// mount source). It also runs again when a stopped container is started.
func TestInitializeCommandRunsBeforeCreateE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"initializeCommand": "mkdir -p generated && echo init >> generated/runs",
		"mounts": ["source=${localWorkspaceFolder}/generated,target=/generated,type=bind"]
	}`, helpers.UniqueTestName(t)))

	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Equal(t, "init", dcxExec(t, ws, "cat", "/generated/runs"))

	helpers.RunDCXInDirSuccess(t, ws, "stop")
	helpers.RunDCXInDirSuccess(t, ws, "up")
	data, err := os.ReadFile(filepath.Join(ws, "generated", "runs"))
	require.NoError(t, err)
	assert.Equal(t, 2, strings.Count(string(data), "init"), "initializeCommand runs on every start")
}
