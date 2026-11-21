package hostconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each SSH agent socket gets its own shared connection: one opened from a
// shell with another agent would forward that agent instead.
func TestControlPathIsPerAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DCX_HOME", home)

	a, ok := controlPath("wk1", "/tmp/ssh-AAA/agent.1")
	require.True(t, ok)
	again, _ := controlPath("wk1", "/tmp/ssh-AAA/agent.1")
	b, _ := controlPath("wk1", "/tmp/ssh-BBB/agent.2")
	none, _ := controlPath("wk1", "")
	other, _ := controlPath("wk2", "/tmp/ssh-AAA/agent.1")

	assert.Equal(t, a, again, "the same agent shares")
	assert.NotEqual(t, a, b, "another agent does not")
	assert.NotEqual(t, a, none)
	assert.NotEqual(t, a, other, "nor another workspace")
	assert.Equal(t, filepath.Join(home, "workspaces", "wk1"), filepath.Dir(a))
	assert.True(t, strings.HasPrefix(filepath.Base(a), "ssh-"))
	info, err := os.Stat(filepath.Dir(a))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "the socket's folder is private")
}

// A path OpenSSH cannot bind would fail the connection itself, so sharing
// is left out then.
func TestShareOptionsSkipsLongPaths(t *testing.T) {
	t.Setenv("DCX_HOME", t.TempDir())
	t.Setenv("SSH_AUTH_SOCK", "/tmp/agent")
	opts := ShareOptions("wk1")
	require.Len(t, opts, 6)
	assert.Equal(t, "ControlMaster=auto", opts[1])
	assert.True(t, strings.HasPrefix(opts[3], "ControlPath="))
	assert.LessOrEqual(t, len(strings.TrimPrefix(opts[3], "ControlPath=")), maxControlPath)

	t.Setenv("DCX_HOME", "/"+strings.Repeat("x", maxControlPath))
	assert.Nil(t, ShareOptions("wk1"))
}
