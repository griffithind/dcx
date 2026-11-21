package hostconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shortHome is a $DCX_HOME short enough for a control socket: t.TempDir is
// under macOS's long /var/folders/…, past the limit.
func shortHome(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "dcx-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// Each SSH agent socket gets its own shared connection: one opened from a
// shell with another agent would forward that agent instead.
func TestShareEnvIsPerAgent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-AAA/agent.1")
	a := ShareEnv()
	assert.Equal(t, a, ShareEnv(), "the same agent shares")
	assert.Regexp(t, `^DCX_SSH_SHARE=[0-9a-f]{8}$`, a)
	t.Setenv("SSH_AUTH_SOCK", "/tmp/ssh-BBB/agent.2")
	assert.NotEqual(t, a, ShareEnv(), "another agent does not")
}

// The block shares dcx's connections (which set DCX_SSH_SHARE) through a
// per-agent socket in the workspace folder, and leaves plain `ssh <alias>`
// and other hosts alone. Checked with OpenSSH itself (ssh -G).
func TestBlockSharesOnlyDCXConnections(t *testing.T) {
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("needs the OpenSSH client")
	}
	home := shortHome(t)
	t.Setenv("DCX_HOME", home)
	config := filepath.Join(t.TempDir(), "config")
	block := renderBlock(Entry{ContainerName: "c", WorkspaceID: "wk1", Port: 2222}, []string{"app.dcx", "c.dcx"}, "127.0.0.1", "")
	require.NoError(t, os.WriteFile(config, []byte(block+"Host other\n  Port 2223\n"), 0o600))

	resolved := func(host string, env ...string) map[string]string {
		cmd := exec.Command("ssh", "-G", "-F", config, host)
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.Output()
		require.NoError(t, err)
		opts := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			if k, v, ok := strings.Cut(line, " "); ok {
				opts[k] = v
			}
		}
		return opts
	}
	for _, alias := range []string{"app.dcx", "c.dcx"} {
		got := resolved(alias, "DCX_SSH_SHARE=0123abcd")
		assert.Equal(t, "auto", got["controlmaster"], alias)
		assert.Equal(t, filepath.Join(home, "workspaces", "wk1", "ssh-0123abcd"), got["controlpath"], alias)
		assert.Equal(t, "600", got["controlpersist"], alias)
	}
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "DCX_SSH_SHARE=") {
			env = append(env, e)
		}
	}
	plain := exec.Command("ssh", "-G", "-F", config, "app.dcx")
	plain.Env = env
	out, err := plain.Output()
	require.NoError(t, err)
	assert.Contains(t, string(out), "controlmaster false", "plain ssh <alias> is not shared")
	assert.Equal(t, "false", resolved("other", "DCX_SSH_SHARE=0123abcd")["controlmaster"], "other hosts are untouched")
}

// A path OpenSSH cannot bind would fail the connection itself, so sharing
// is left out then.
func TestShareSkipsLongPaths(t *testing.T) {
	t.Setenv("DCX_HOME", shortHome(t))
	assert.Contains(t, renderShare("wk1", []string{"a.dcx"}), "ControlPath")
	t.Setenv("DCX_HOME", "/"+strings.Repeat("x", maxControlPath))
	assert.Empty(t, renderShare("wk1", []string{"a.dcx"}))
}
