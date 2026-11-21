//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plainSSH runs OpenSSH against the generated config, the way users and
// editors connect.
func plainSSH(t *testing.T, alias string, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := helpers.SSHCommand(append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", alias}, args...)...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// TestAgentFilesRootOwnedE2E: the agent binary, its state and the shell
// integration live in root-owned directories, not world-writable /tmp
// where a container user could plant symlinks for the root agent to follow.
func TestAgentFilesRootOwnedE2E(t *testing.T) {
	t.Parallel()

	env := alpineEnv(t)
	ws := env.Dir
	c := helpers.ContainerName(t, ws)

	out, err := exec.Command("docker", "exec", c, "stat", "-c", "%n %U %a",
		"/var/lib/dcx", "/var/lib/dcx/bin", "/var/lib/dcx/bin/dcx-agent", "/run/dcx", "/run/dcx/shell", "/run/dcx/session.json").CombinedOutput()
	require.NoError(t, err, string(out))
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		require.Len(t, fields, 3, line)
		assert.Equal(t, "root", fields[1], "%s must be root-owned", fields[0])
		assert.NotContains(t, []string{"777", "775", "757"}, fields[2], "%s must not be writable by others", fields[0])
	}

	out, err = exec.Command("docker", "exec", c, "sh", "-c",
		`ls /tmp/dcx-agent /tmp/dcx-agent.log /tmp/.dcx-shell 2>/dev/null; true`).CombinedOutput()
	require.NoError(t, err)
	assert.Empty(t, strings.TrimSpace(string(out)), "nothing of the agent's may live in /tmp")
}

// TestNonRootRemoteUserSessionsE2E: sessions of a non-root remoteUser keep
// its supplementary groups and get a terminal it owns (sudo, ssh and gpg
// reopen /dev/tty); with updateRemoteUserUID false its UID stays the
// image's.
func TestNonRootRemoteUserSessionsE2E(t *testing.T) {
	t.Parallel()
	env := devUserEnv(t)
	ws := env.Dir
	alias := extractSSHHostname(t, env.UpOutput)

	assert.Equal(t, "dev", dcxExec(t, ws, "id", "-un"))
	assert.Equal(t, "1234", dcxExec(t, ws, "id", "-u"), "updateRemoteUserUID false keeps the image's UID")
	assert.Contains(t, strings.Fields(dcxExec(t, ws, "id", "-Gn")), "extra", "supplementary groups must survive the privilege drop")
	out, err := plainSSH(t, alias, "", "-tt", `stat -c %U "$(tty)"`)
	require.NoError(t, err, out)
	assert.Equal(t, "dev", strings.TrimSpace(out), "the session's terminal must belong to the session user")
}

// TestSameNameWorkspacesGetDistinctAliasesE2E: two checkouts sharing a
// "name" each reach their own container — the first by "<name>.dcx", the
// second by its unique "<container>.dcx" — and `dcx ssh` picks the right
// one from the current directory.
func TestSameNameWorkspacesGetDistinctAliasesE2E(t *testing.T) {
	t.Parallel()

	// The first checkout is alpineEnv; the second copies its config.
	env := alpineEnv(t)
	first := env.Dir
	cfg, err := os.ReadFile(filepath.Join(first, ".devcontainer", "devcontainer.json"))
	require.NoError(t, err)
	second := helpers.CreateTempWorkspace(t, string(cfg))
	marker := "marker-" + helpers.UniqueTestName(t)
	require.NoError(t, os.WriteFile(filepath.Join(first, marker), []byte("first"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(second, marker), []byte("second"), 0o644))
	t.Cleanup(func() { _ = os.Remove(filepath.Join(first, marker)) })

	firstAlias := extractSSHHostname(t, env.UpOutput)
	secondAlias := extractSSHHostname(t, helpers.RunDCXInDirSuccess(t, second, "up"))
	assert.Equal(t, env.Name+".dcx", firstAlias)
	assert.NotEqual(t, firstAlias, secondAlias)

	for alias, want := range map[string]string{firstAlias: "first", secondAlias: "second"} {
		out, err := plainSSH(t, alias, "", "cat", "/workspace/"+marker)
		require.NoError(t, err, out)
		assert.Equal(t, want, strings.TrimSpace(out), "ssh %s", alias)
	}
	assert.Equal(t, "first", dcxExec(t, first, "cat", "/workspace/"+marker))
	assert.Equal(t, "second", dcxExec(t, second, "cat", "/workspace/"+marker))
}
