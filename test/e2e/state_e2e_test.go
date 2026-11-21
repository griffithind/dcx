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

// inspectFile returns a file in the container, read as root.
func inspectFile(t *testing.T, container, path string) string {
	t.Helper()
	out, err := exec.Command("docker", "exec", "--user", "root", container, "cat", path).CombinedOutput()
	require.NoError(t, err, string(out))
	return strings.TrimSpace(string(out))
}

// TestStateLifecycleE2E walks one workspace through every state (absent,
// running, stopped, and back), checking in each what state, status, doctor,
// ssh, stop and down do there.
//
// Every start re-establishes the environment: `dcx up` on a stopped
// container and `dcx restart` used to only run `docker start`, but the
// in-container SSH agent dies with the container, so sessions must keep
// working afterwards and postStartCommand must run on every start.
// initializeCommand runs on the host on every up (also with the container
// running, M6) and before the container is created, so it can prepare what
// the container needs at creation time (here: a bind mount source). The
// keep-alive handles SIGTERM, so stopping does not wait out Docker's 10s
// kill timeout (a container killed after it exits 137).
func TestStateLifecycleE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"initializeCommand": "mkdir -p generated && echo init >> generated/runs",
		"mounts": ["source=${localWorkspaceFolder}/generated,target=/generated,type=bind"],
		"postStartCommand": "echo started >> /tmp/poststart.log"
	}`, helpers.UniqueTestName(t)))
	state := func() string { return helpers.GetContainerState(t, workspace) }
	initRuns := func(t *testing.T) int {
		data, err := os.ReadFile(filepath.Join(workspace, "generated", "runs"))
		require.NoError(t, err)
		return strings.Count(string(data), "init")
	}
	postStartRuns := func(t *testing.T) int {
		return strings.Count(dcxExec(t, workspace, "cat", "/tmp/poststart.log"), "started")
	}

	t.Run("absent", func(t *testing.T) {
		assert.Equal(t, "absent", state())
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		for _, want := range []string{"State:", "absent", "Workspace:", "Workspace ID:"} {
			assert.Contains(t, stdout, want)
		}
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "doctor")
		require.NoError(t, err, "doctor works without a container")
		assert.Contains(t, stdout, "Docker")
		_, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "test")
		assert.Error(t, err, "ssh fails when no container exists")
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "down"), "No devcontainer found", "down is idempotent")
	})

	t.Run("absent_to_running", func(t *testing.T) {
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "up"), "Devcontainer started successfully")
		assert.Equal(t, "running", state())
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		for _, want := range []string{"State:", "running", "Primary Container", "ID:", "Name:"} {
			assert.Contains(t, stdout, want)
		}
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "doctor")
		require.NoError(t, err)
		assert.Contains(t, stdout, "Docker")
		assert.Equal(t, "init", dcxExec(t, workspace, "cat", "/generated/runs"), "initializeCommand ran before create")
		assert.Equal(t, 1, postStartRuns(t))
	})

	t.Run("up_while_running", func(t *testing.T) {
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "up"), "already running")
		assert.Equal(t, 2, initRuns(t), "initializeCommand runs with the container running")
		assert.Equal(t, 1, postStartRuns(t))
	})

	// An agent from another dcx build that stopped answering is replaced
	// and restarted on the next up, without rerunning postStartCommand.
	t.Run("outdated_agent_replaced", func(t *testing.T) {
		name := helpers.ContainerName(t, workspace)
		sidecar := "/var/lib/dcx/bin/dcx-agent.sha256"
		current := inspectFile(t, name, sidecar)
		out, err := exec.Command("docker", "exec", "--user", "root", name, "sh", "-c",
			"echo outdated > "+sidecar+"; pkill -f '[d]cx-agent listen'; true").CombinedOutput()
		require.NoError(t, err, string(out))
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "up"), "reconnecting")
		assert.Equal(t, current, inspectFile(t, name, sidecar), "the current agent is installed again")
		assert.Equal(t, 1, postStartRuns(t))
		assert.Equal(t, 3, initRuns(t))
	})

	t.Run("running_to_stopped", func(t *testing.T) {
		name := helpers.ContainerName(t, workspace)
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "stop"), "stopped")
		assert.Equal(t, "created", state())
		assert.Equal(t, "0", inspect(t, name, "{{.State.ExitCode}}"), "the keep-alive must exit on SIGTERM, not be killed (137)")
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.Contains(t, stdout, "State:")
		assert.Contains(t, stdout, "created")
		_, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "test")
		assert.Error(t, err, "ssh fails on a stopped container")
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "stop"), "already stopped", "stop is idempotent")
	})

	t.Run("stopped_to_running", func(t *testing.T) {
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "up"), "started")
		assert.Equal(t, "running", state())
		assert.Equal(t, 2, postStartRuns(t), "postStartCommand runs on every start")
		assert.Equal(t, 4, initRuns(t), "initializeCommand runs when a stopped container starts")
	})

	t.Run("restart", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "restart")
		assert.Equal(t, "running", state())
		assert.Equal(t, 3, postStartRuns(t), "postStartCommand runs after restart")
	})

	t.Run("running_to_absent", func(t *testing.T) {
		assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "down"), "removed")
		assert.Equal(t, "absent", state())
	})

	t.Run("up_from_absent", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Equal(t, "running", state())
	})

	t.Run("stopped_to_absent", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Equal(t, "created", state())
		helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Equal(t, "absent", state())
	})
}
