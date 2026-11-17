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

// TestEnvValuesAreNotExecutedE2E: environment values reach /etc/environment
// and the probe as data. Both used to be spliced into root shell scripts,
// so a quote or a heredoc terminator in a value ran commands as root.
func TestEnvValuesAreNotExecutedE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	evil := `x' ; touch /tmp/pwned-probe ; echo '`
	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"userEnvProbe": "loginShell",
		"containerEnv": {"EVIL": %q, "MULTI": "a\nDCXEOF\ntouch /tmp/pwned-heredoc"}
	}`, helpers.UniqueTestName(t), evil))
	t.Cleanup(func() { helpers.RunDCXInDir(t, ws, "down") })
	helpers.RunDCXInDirSuccess(t, ws, "up")
	c := helpers.ContainerName(t, ws)

	out, _ := exec.Command("docker", "exec", c, "sh", "-c", "ls /tmp/pwned-* 2>/dev/null; true").CombinedOutput()
	assert.Empty(t, strings.TrimSpace(string(out)), "no injected command may have run")

	envFile, err := exec.Command("docker", "exec", c, "cat", "/etc/environment").CombinedOutput()
	require.NoError(t, err, string(envFile))
	assert.Contains(t, string(envFile), `EVIL="`+evil+`"`)
	assert.NotContains(t, string(envFile), "MULTI=", "multi-line values cannot be represented and are skipped")
}

// TestEtcEnvironmentFollowsConfigE2E: dcx's block in /etc/environment
// holds the container's environment (image ENV and containerEnv), as
// devcontainers/cli writes it, and never remoteEnv, which often holds
// tokens and the file is world-readable (M3). The block is rewritten on
// each start, so changed values replace old ones.
func TestEtcEnvironmentFollowsConfigE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	name := helpers.UniqueTestName(t)
	config := func(value string) string {
		return fmt.Sprintf(`{"name": %q, "image": "alpine:3.24", "workspaceFolder": "/workspace", "containerEnv": {"FROM_CONTAINER": %q}, "remoteEnv": {"SECRET_TOKEN": "s3cret"}}`, name, value)
	}
	ws := helpers.CreateTempWorkspace(t, config("one"))
	helpers.RunDCXInDirSuccess(t, ws, "up")

	read := func() string {
		out, err := exec.Command("docker", "exec", helpers.ContainerName(t, ws), "cat", "/etc/environment").CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	first := read()
	assert.Contains(t, first, `FROM_CONTAINER="one"`, "containerEnv is written")
	assert.Contains(t, first, `PATH="`, "the image's environment is written")
	assert.NotContains(t, first, "SECRET_TOKEN", "remoteEnv is not written")

	stdout, stderr, err := helpers.RunDCXInDir(t, ws, "ssh", "--", "printenv", "SECRET_TOKEN")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, "s3cret", strings.TrimSpace(stdout), "sessions still get remoteEnv")

	require.NoError(t, os.WriteFile(filepath.Join(ws, ".devcontainer", "devcontainer.json"), []byte(config("two")), 0o644))
	helpers.RunDCXInDirSuccess(t, ws, "up")
	content := read()
	assert.Contains(t, content, `FROM_CONTAINER="two"`)
	assert.NotContains(t, content, `FROM_CONTAINER="one"`)
	assert.Equal(t, 1, strings.Count(content, "# BEGIN dcx managed environment"))
}
