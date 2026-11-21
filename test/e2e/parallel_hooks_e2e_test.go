//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLifecycleCommandFormatsE2E brings up one container whose hooks use
// every command form: initializeCommand and onCreateCommand as objects
// (parallel, on the host and in the container), updateContentCommand as a
// string, postCreateCommand as an object mixing strings and an array, and
// postStartCommand as an array.
//
// The configuration is debianEnv's. postCreateCommand's cmd1-cmd3 prove they ran concurrently without timing
// thresholds: each checks in, then waits for the other two. Run in
// sequence, the first would wait in vain and fail the hook (after a
// generous 20 s), so `up` succeeding with every "saw all" file present
// means they ran at the same time.
func TestLifecycleCommandFormatsE2E(t *testing.T) {
	t.Parallel()
	workspace := debianEnv(t).Dir

	// initializeCommand ran on the host, in the workspace.
	assert.FileExists(t, workspace+"/.init1-marker", "initializeCommand init1")
	assert.FileExists(t, workspace+"/.init2-marker", "initializeCommand init2")

	for file, want := range map[string]string{
		"/tmp/oncreate1.txt":     "oncreate1",
		"/tmp/oncreate2.txt":     "oncreate2",
		"/tmp/updatecontent.txt": "string-format",
		"/tmp/cmd1.txt":          "cmd1",
		"/tmp/cmd2.txt":          "cmd2",
		"/tmp/cmd3.txt":          "cmd3",
		"/tmp/array.txt":         "array-in-object",
		"/tmp/poststart.txt":     "array-format",
	} {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", file)
		require.NoError(t, err, file)
		assert.Contains(t, stdout, want, file)
	}

	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "sh", "-c", "ls /tmp/saw-all-* | wc -l")
	require.NoError(t, err)
	assert.Equal(t, "3", strings.TrimSpace(stdout), "every command must have seen the other two running")
}
