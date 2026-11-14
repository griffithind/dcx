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

// sshEcho runs `echo marker` in the workspace's container over SSH.
func sshEcho(t *testing.T, workspace, marker string) {
	t.Helper()
	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", marker)
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stdout, marker)
}

// TestHostNetworkE2E guards against the agent's port being published with
// host networking, where Docker discards published ports: the agent must
// listen on the workspace's host port itself.
func TestHostNetworkE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"runArgs": ["--network=host"],
		"postCreateCommand": "echo hooks-ran > /tmp/hooks"
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	sshEcho(t, workspace, "host-network-ok")
	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/hooks")
	require.NoError(t, err, stderr)
	assert.Contains(t, stdout, "hooks-ran", "lifecycle hooks reach the agent too")
}

// TestComposeSharedNetworkE2E guards against the agent's port being
// published on a service that shares another service's network namespace
// (network_mode: service:db), which Docker rejects.
func TestComposeSharedNetworkE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`, helpers.UniqueTestName(t))
	dockerComposeYAML := `services:
  app:
    image: alpine:3.24
    command: sleep infinity
    network_mode: service:db
    volumes:
      - ..:/workspace:cached
  db:
    image: alpine:3.24
    command: sleep infinity
`
	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	sshEcho(t, workspace, "shared-network-ok")
}

// TestRecreateRemovesRenamedComposeServiceE2E guards against a recreate
// leaving the old primary running after its service was renamed: compose
// down runs with the edited files, so the old service is an orphan.
func TestRecreateRemovesRenamedComposeServiceE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	name := helpers.UniqueTestName(t)
	config := func(service string) string {
		return fmt.Sprintf(`{
			"name": %q,
			"dockerComposeFile": "docker-compose.yml",
			"service": %q,
			"workspaceFolder": "/workspace"
		}`, name, service)
	}
	compose := func(service string) string {
		return fmt.Sprintf(`services:
  %s:
    image: alpine:3.24
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`, service)
	}
	workspace := helpers.CreateTempComposeWorkspace(t, config("app"), compose("app"))
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down", "--remove-orphans") })
	helpers.RunDCXInDirSuccess(t, workspace, "up")
	project := helpers.GetStatus(t, workspace).ResourceName

	dir := filepath.Join(workspace, ".devcontainer")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(config("dev")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte(compose("dev")), 0o644))
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	out, err := exec.Command("docker", "ps", "-a", "--format", "{{.Label \"com.docker.compose.service\"}}",
		"--filter", "label=com.docker.compose.project="+project).Output()
	require.NoError(t, err)
	assert.Equal(t, "dev", strings.TrimSpace(string(out)), "only the renamed service remains")
	sshEcho(t, workspace, "renamed-service-ok")
}

// TestRootRemoteUserOnNonRootImageE2E guards against the agent running as
// the image's (non-root) USER: it can neither read a host key owned by
// remoteUser nor switch to root for sessions.
func TestRootRemoteUserOnNonRootImageE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"remoteUser": "root",
		"updateRemoteUserUID": false
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)
	dockerfile := "FROM alpine:3.24\nRUN adduser -D app\nUSER app\n"
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".devcontainer", "Dockerfile"), []byte(dockerfile), 0o644))

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "id", "-un")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, "root", strings.TrimSpace(stdout))
}

// TestHostsPersistAcrossRestartE2E guards against `dcx restart` relaunching
// the agent loopback-only while the port stays published on every
// interface: --hosts is recorded on the container and kept, and changing it
// needs a recreate.
func TestHostsPersistAcrossRestartE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := helpers.CreateTempWorkspace(t, helpers.SimpleImageConfig(t, "alpine:3.24"))
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down") })

	agentArgs := func(t *testing.T) string {
		t.Helper()
		stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "sh", "-c",
			`for f in /proc/[0-9]*/cmdline; do tr '\0' ' ' < "$f"; echo; done | grep 'dcx-agent listen'`)
		require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
		return stdout
	}

	helpers.RunDCXInDirSuccess(t, workspace, "up", "--hosts=10.99.0.0/24")
	assert.Contains(t, agentArgs(t), "--allow-cidrs 10.99.0.0/24")

	helpers.RunDCXInDirSuccess(t, workspace, "restart")
	assert.Contains(t, agentArgs(t), "--allow-cidrs 10.99.0.0/24", "restart keeps --hosts")

	_, stderr, err := helpers.RunDCXInDir(t, workspace, "up", "--hosts=loopback")
	require.Error(t, err, "changing --hosts without --recreate must fail")
	assert.Contains(t, stderr, "--recreate")

	helpers.RunDCXInDirSuccess(t, workspace, "up", "--recreate", "--hosts=loopback")
	assert.NotContains(t, agentArgs(t), "--allow-cidrs")
}
