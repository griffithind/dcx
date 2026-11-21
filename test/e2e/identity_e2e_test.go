//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// containerID returns the ID of the workspace's primary container.
func containerID(t *testing.T, workspace string) string {
	t.Helper()
	name := helpers.ContainerName(t, workspace)
	require.NotEmpty(t, name)
	out, err := exec.Command("docker", "inspect", "--format", "{{.Id}}", name).Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

// TestConfigEditsE2E walks one workspace through config edits.
//   - dcx's block in /etc/environment holds the container's environment
//     (image ENV and containerEnv), as devcontainers/cli writes it, and
//     never remoteEnv, which often holds tokens while the file is
//     world-readable (M3).
//   - Environment values reach /etc/environment and the probe as data: both
//     used to be spliced into root shell scripts, so a quote or a heredoc
//     terminator in a value ran commands as root.
//   - Edits that don't affect the container (comments, formatting, editor
//     settings) leave it current; edits that do make it stale, and up
//     recreates it with the new values.
func TestConfigEditsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	name := helpers.UniqueTestName(t)
	evil := `x' ; touch /tmp/pwned-probe ; echo '`
	config := func(value, extra string) string {
		return fmt.Sprintf(`{
			%s
			"name": %q,
			"image": "alpine:3.24",
			"workspaceFolder": "/workspace",
			"userEnvProbe": "loginShell",
			"containerEnv": {"FROM_CONTAINER": %q, "EVIL": %q, "MULTI": "a\nDCXEOF\ntouch /tmp/pwned-heredoc"},
			"remoteEnv": {"SECRET_TOKEN": "s3cret"}
		}`, extra, name, value, evil)
	}
	ws := helpers.CreateTempWorkspace(t, config("one", ""))
	write := func(body string) {
		require.NoError(t, os.WriteFile(filepath.Join(ws, ".devcontainer", "devcontainer.json"), []byte(body), 0o644))
	}
	read := func() string {
		out, err := exec.Command("docker", "exec", helpers.ContainerName(t, ws), "cat", "/etc/environment").CombinedOutput()
		require.NoError(t, err, string(out))
		return string(out)
	}

	helpers.RunDCXInDirSuccess(t, ws, "up")
	first := containerID(t, ws)
	env := read()
	assert.Contains(t, env, `FROM_CONTAINER="one"`, "containerEnv is written")
	assert.Contains(t, env, `PATH="`, "the image's environment is written")
	assert.NotContains(t, env, "SECRET_TOKEN", "remoteEnv is not written")
	assert.Contains(t, env, `EVIL="`+evil+`"`)
	assert.NotContains(t, env, "MULTI=", "multi-line values cannot be represented and are skipped")
	out, _ := exec.Command("docker", "exec", helpers.ContainerName(t, ws), "sh", "-c", "ls /tmp/pwned-* 2>/dev/null; true").CombinedOutput()
	assert.Empty(t, strings.TrimSpace(string(out)), "no injected command may have run")
	assert.Equal(t, "s3cret", dcxExec(t, ws, "printenv", "SECRET_TOKEN"), "sessions still get remoteEnv")

	write(config("one", `// only a comment, formatting and editor settings changed
			"customizations": {"vscode": {"extensions": ["golang.go"]}},`))
	assert.Equal(t, "running", helpers.GetContainerState(t, ws))
	assert.Contains(t, helpers.RunDCXInDirSuccess(t, ws, "up"), "already running")
	assert.Equal(t, first, containerID(t, ws), "cosmetic edits must not recreate the container")

	write(config("two", ""))
	assert.Equal(t, "stale", helpers.GetContainerState(t, ws))
	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Equal(t, "running", helpers.GetContainerState(t, ws))
	assert.NotEqual(t, first, containerID(t, ws), "a containerEnv change must recreate")
	assert.Equal(t, "two", dcxExec(t, ws, "printenv", "FROM_CONTAINER"))
	env = read()
	assert.Contains(t, env, `FROM_CONTAINER="two"`)
	assert.NotContains(t, env, `FROM_CONTAINER="one"`)
	assert.Equal(t, 1, strings.Count(env, "# BEGIN dcx managed environment"))
}

// TestFeaturesUIDAndLocalFeatureEditsE2E walks a Dockerfile config with a
// local feature and a UID-updated remoteUser through its caching: a second
// up keeps the container; down and up reuse the cached images and the UID
// layer still applies (it used to be skipped on cache hits); editing the
// local feature rebuilds it; changing its options in devcontainer.json
// builds a new image with the new values.
func TestFeaturesUIDAndLocalFeatureEditsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	hostUID := os.Getuid()
	if hostUID == 0 || hostUID == 4321 {
		t.Skip("needs a non-root host user whose UID differs from the image user's")
	}

	config := func(greeting string) string {
		return fmt.Sprintf(`{
			"name": %q,
			"build": {"dockerfile": "Dockerfile"},
			"workspaceFolder": "/workspace",
			"remoteUser": "dev",
			"features": {"./marker": {"greeting": %q}}
		}`, helpers.UniqueTestName(t), greeting)
	}
	ws := helpers.CreateTempWorkspace(t, config("hi"))
	dc := filepath.Join(ws, ".devcontainer")
	require.NoError(t, os.WriteFile(filepath.Join(dc, "Dockerfile"), []byte("FROM alpine:3.24\nRUN adduser -D -u 4321 dev\n"), 0o644))
	feat := filepath.Join(dc, "marker")
	require.NoError(t, os.MkdirAll(feat, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(feat, "devcontainer-feature.json"), []byte(`{
		"id": "marker", "version": "1.0.0",
		"options": {"greeting": {"type": "string", "default": "hello"}}
	}`), 0o644))
	writeInstall := func(v string) {
		require.NoError(t, os.WriteFile(filepath.Join(feat, "install.sh"), []byte("#!/bin/sh\necho "+v+" > /marker\necho \"$GREETING\" > /greeting\n"), 0o755))
	}
	writeInstall("v1")
	want := strconv.Itoa(hostUID)

	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Equal(t, want, dcxExec(t, ws, "id", "-u", "dev"))
	assert.Equal(t, "v1", dcxExec(t, ws, "cat", "/marker"))
	assert.Equal(t, "hi", dcxExec(t, ws, "cat", "/greeting"))
	containerID := helpers.ContainerID(t, ws)

	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Equal(t, containerID, helpers.ContainerID(t, ws), "a second up keeps the container")

	helpers.RunDCXInDirSuccess(t, ws, "down")
	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Equal(t, want, dcxExec(t, ws, "id", "-u", "dev"), "UID layer missing after cache hit")

	writeInstall("v2")
	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Equal(t, "v2", dcxExec(t, ws, "cat", "/marker"), "an edited local feature is rebuilt")
	assert.Equal(t, want, dcxExec(t, ws, "id", "-u", "dev"))

	imageID := helpers.ContainerImageID(t, ws)
	require.NoError(t, os.WriteFile(filepath.Join(dc, "devcontainer.json"), []byte(config("changed")), 0o644))
	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.NotEqual(t, imageID, helpers.ContainerImageID(t, ws), "changed options build a new image")
	assert.Equal(t, "changed", dcxExec(t, ws, "cat", "/greeting"))
}
