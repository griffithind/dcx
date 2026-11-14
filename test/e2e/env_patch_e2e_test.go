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
		"containerEnv": {"EVIL": %q},
		"remoteEnv": {"MULTI": "a\nDCXEOF\ntouch /tmp/pwned-heredoc"}
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

// TestEtcEnvironmentFollowsConfigE2E: dcx's block in /etc/environment is
// rewritten on each start, so changed values replace old ones (it used to be
// appended once and never updated).
func TestEtcEnvironmentFollowsConfigE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	name := helpers.UniqueTestName(t)
	config := func(value string) string {
		return fmt.Sprintf(`{"name": %q, "image": "alpine:3.24", "workspaceFolder": "/workspace", "containerEnv": {"FROM_CONTAINER": "container-value"}, "remoteEnv": {"STAGE": %q}}`, name, value)
	}
	ws := helpers.CreateTempWorkspace(t, config("one"))
	helpers.RunDCXInDirSuccess(t, ws, "up")
	c := helpers.ContainerName(t, ws)

	read := func() string {
		out, err := exec.Command("docker", "exec", c, "cat", "/etc/environment").CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}
	first := read()
	assert.Contains(t, first, `STAGE="one"`, "remoteEnv is written")
	assert.Contains(t, first, `FROM_CONTAINER="container-value"`, "containerEnv is written")

	require.NoError(t, os.WriteFile(filepath.Join(ws, ".devcontainer", "devcontainer.json"), []byte(config("two")), 0o644))
	helpers.RunDCXInDirSuccess(t, ws, "restart")
	content := read()
	assert.Contains(t, content, `STAGE="two"`)
	assert.NotContains(t, content, `STAGE="one"`)
	assert.Equal(t, 1, strings.Count(content, "# BEGIN dcx managed environment"))
}
