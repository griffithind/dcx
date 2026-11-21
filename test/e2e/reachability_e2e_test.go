//go:build e2e

package e2e

import (
	"fmt"
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

// TestHostsPersistAcrossRestartE2E guards against `dcx restart` relaunching
// the agent loopback-only while the port stays published on every
// interface: --hosts is recorded on the container and kept, and changing it
// needs a recreate.
//
// The SSH host port stays the same across a recreate and across down and
// up: IDE clients (VS Code Remote-SSH, JetBrains Gateway, Claude Desktop)
// key known_hosts entries and connection caches by (host, port), so a
// rotating port shows up as "connection refused" or a host-key warning.
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

	port := extractSSHPort(t, helpers.RunDCXInDirSuccess(t, workspace, "up", "--hosts=10.99.0.0/24"))
	assert.Contains(t, agentArgs(t), "--allow-cidrs 10.99.0.0/24")

	helpers.RunDCXInDirSuccess(t, workspace, "restart")
	assert.Contains(t, agentArgs(t), "--allow-cidrs 10.99.0.0/24", "restart keeps --hosts")

	_, stderr, err := helpers.RunDCXInDir(t, workspace, "up", "--hosts=loopback")
	require.Error(t, err, "changing --hosts without --recreate must fail")
	assert.Contains(t, stderr, "--recreate")

	assert.Equal(t, port, extractSSHPort(t, helpers.RunDCXInDirSuccess(t, workspace, "up", "--recreate", "--hosts=loopback")),
		"the SSH port is stable across a recreate")
	assert.NotContains(t, agentArgs(t), "--allow-cidrs")

	helpers.RunDCXInDirSuccess(t, workspace, "down")
	assert.Equal(t, port, extractSSHPort(t, helpers.RunDCXInDirSuccess(t, workspace, "up")),
		"the SSH port is stable across down and up")
}
