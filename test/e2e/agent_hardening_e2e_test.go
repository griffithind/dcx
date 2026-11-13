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

// plainSSH runs OpenSSH against the generated config, the way users and
// editors connect.
func plainSSH(t *testing.T, alias string, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("ssh", append([]string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=10", alias}, args...)...)
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
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, helpers.SimpleImageConfig(t, "alpine:latest"))
	t.Cleanup(func() { helpers.RunDCXInDir(t, ws, "down") })
	helpers.RunDCXInDirSuccess(t, ws, "up")
	c := primaryContainer(t, ws)

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

// TestPlainSSHSessionParityE2E: a plain OpenSSH session gets what
// `dcx ssh` gives — remoteEnv (with ${containerEnv:…}) and,
// for interactive sessions only, postAttachCommand.
func TestPlainSSHSessionParityE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"containerEnv": {"BASE": "base-value"},
		"remoteEnv": {"FROM_REMOTE": "remote-${containerEnv:BASE}"},
		"postAttachCommand": "echo attached >> /tmp/attach.log"
	}`, helpers.UniqueTestName(t)))
	t.Cleanup(func() { helpers.RunDCXInDir(t, ws, "down") })
	stdout := helpers.RunDCXInDirSuccess(t, ws, "up")
	alias := extractSSHHostname(t, stdout)

	out, err := plainSSH(t, alias, "", "printenv", "FROM_REMOTE")
	require.NoError(t, err, out)
	assert.Equal(t, "remote-base-value", strings.TrimSpace(out))

	_, _, err = helpers.RunDCXInDir(t, ws, "ssh", "--", "test", "-f", "/tmp/attach.log")
	assert.Error(t, err, "postAttachCommand must not run for exec sessions")

	out, err = plainSSH(t, alias, "exit\n", "-tt")
	require.NoError(t, err, out)
	assert.Equal(t, "attached", dcxExec(t, ws, "cat", "/tmp/attach.log"))

	// An interactive `dcx ssh` gets it from the agent as well, once per attach.
	shellOut, err := helpers.RunDCXInPTY(t, ws, "exit\n", "ssh")
	require.NoError(t, err, shellOut)
	assert.Equal(t, "attached\nattached", dcxExec(t, ws, "cat", "/tmp/attach.log"))
}

// TestNonRootRemoteUserSessionsE2E: with a root agent and a non-root
// remoteUser, sessions keep the user's supplementary groups and get a
// terminal they own (sudo, ssh and gpg reopen /dev/tty).
func TestNonRootRemoteUserSessionsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"remoteUser": "dev",
		"updateRemoteUserUID": false
	}`, helpers.UniqueTestName(t)))
	dockerfile := "FROM alpine:latest\nRUN adduser -D dev && addgroup extra && addgroup dev extra\n"
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".devcontainer", "Dockerfile"), []byte(dockerfile), 0o644))
	t.Cleanup(func() { helpers.RunDCXInDir(t, ws, "down") })
	stdout := helpers.RunDCXInDirSuccess(t, ws, "up")
	alias := extractSSHHostname(t, stdout)

	assert.Equal(t, "dev", dcxExec(t, ws, "id", "-un"))
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
	helpers.RequireDockerAvailable(t)

	cfg := helpers.SimpleImageConfig(t, "alpine:latest")
	first := helpers.CreateTempWorkspace(t, cfg)
	second := helpers.CreateTempWorkspace(t, cfg)
	require.NoError(t, os.WriteFile(filepath.Join(first, "marker"), []byte("first"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(second, "marker"), []byte("second"), 0o644))
	t.Cleanup(func() {
		helpers.RunDCXInDir(t, first, "down")
		helpers.RunDCXInDir(t, second, "down")
	})

	firstAlias := extractSSHHostname(t, helpers.RunDCXInDirSuccess(t, first, "up"))
	secondAlias := extractSSHHostname(t, helpers.RunDCXInDirSuccess(t, second, "up"))
	assert.NotEqual(t, firstAlias, secondAlias)

	for alias, want := range map[string]string{firstAlias: "first", secondAlias: "second"} {
		out, err := plainSSH(t, alias, "", "cat", "/workspace/marker")
		require.NoError(t, err, out)
		assert.Equal(t, want, strings.TrimSpace(out), "ssh %s", alias)
	}
	assert.Equal(t, "first", dcxExec(t, first, "cat", "/workspace/marker"))
	assert.Equal(t, "second", dcxExec(t, second, "cat", "/workspace/marker"))
}
