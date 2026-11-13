//go:build e2e

package e2e

import (
	"fmt"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParallelPostCreateCommandE2E tests that postCreateCommand with object syntax
// runs all named commands in parallel per the devcontainer spec.
func TestParallelPostCreateCommandE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Use object syntax for postCreateCommand - commands should run in parallel
	devcontainerJSON := `{
		"name": "Parallel Hooks Test",
		"image": "debian:bookworm-slim",
		"workspaceFolder": "/workspace",
		"postCreateCommand": {
			"task1": "echo task1-start && sleep 0.5 && echo task1 > /tmp/task1.txt && echo task1-done",
			"task2": "echo task2-start && sleep 0.5 && echo task2 > /tmp/task2.txt && echo task2-done",
			"task3": ["sh", "-c", "echo task3 > /tmp/task3.txt"]
		}
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify all three tasks executed
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/task1.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "task1", "task1 should have executed")

	stdout, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/task2.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "task2", "task2 should have executed")

	stdout, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/task3.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "task3", "task3 should have executed")
}

// TestParallelPostCreateCommandTimingE2E proves object-form commands run
// concurrently without timing thresholds: each command checks in, then
// waits for the other two. Run in sequence, the first one would wait in
// vain and fail the hook (after a generous 20s), so `up` succeeding with
// every "saw all" file present means they ran at the same time.
func TestParallelPostCreateCommandTimingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	rendezvous := func(name string) string {
		return `mkdir -p /tmp/rv && touch /tmp/rv/` + name + ` && i=0; while [ "$(ls /tmp/rv | wc -l)" -lt 3 ]; do i=$((i+1)); [ $i -gt 200 ] && exit 1; sleep 0.1; done; touch /tmp/saw-all-` + name
	}
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "debian:bookworm-slim",
		"workspaceFolder": "/workspace",
		"postCreateCommand": {"cmd1": %q, "cmd2": %q, "cmd3": %q}
	}`, helpers.UniqueTestName(t), rendezvous("cmd1"), rendezvous("cmd2"), rendezvous("cmd3"))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down") })

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "sh", "-c", "ls /tmp/saw-all-* | wc -l")
	require.NoError(t, err)
	assert.Equal(t, "3", strings.TrimSpace(stdout), "every command must have seen the other two running")
}

// TestParallelOnCreateCommandE2E tests that onCreateCommand with object syntax
// also supports parallel execution.
func TestParallelOnCreateCommandE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Parallel OnCreate Test",
		"image": "debian:bookworm-slim",
		"workspaceFolder": "/workspace",
		"onCreateCommand": {
			"setup1": "echo oncreate1 > /tmp/oncreate1.txt",
			"setup2": "echo oncreate2 > /tmp/oncreate2.txt"
		}
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify both onCreateCommand tasks executed
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/oncreate1.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "oncreate1", "onCreateCommand task1 should have executed")

	stdout, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/oncreate2.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "oncreate2", "onCreateCommand task2 should have executed")
}

// TestParallelInitializeCommandE2E tests that initializeCommand with object syntax
// runs commands in parallel on the host.
func TestParallelInitializeCommandE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create workspace first so we can write marker files there
	devcontainerJSON := `{
		"name": "Parallel Initialize Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"initializeCommand": {
			"init1": "touch .init1-marker",
			"init2": "touch .init2-marker"
		}
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify both initializeCommand tasks executed (on host, in workspace dir)
	assert.FileExists(t, workspace+"/.init1-marker", "initializeCommand init1 should have created marker")
	assert.FileExists(t, workspace+"/.init2-marker", "initializeCommand init2 should have created marker")
}

// TestMixedCommandFormatsE2E tests that different command formats work together.
func TestMixedCommandFormatsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Mixed Commands Test",
		"image": "debian:bookworm-slim",
		"workspaceFolder": "/workspace",
		"onCreateCommand": "echo string-format > /tmp/oncreate.txt",
		"postCreateCommand": {
			"parallel1": "echo parallel1 > /tmp/parallel1.txt",
			"parallel2": ["sh", "-c", "echo parallel2 > /tmp/parallel2.txt"]
		},
		"postStartCommand": ["sh", "-c", "echo array-format > /tmp/poststart.txt"]
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify string format worked
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/oncreate.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "string-format")

	// Verify parallel object format worked
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/parallel1.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "parallel1")

	stdout, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/parallel2.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "parallel2")

	// Verify array format worked
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/poststart.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "array-format")
}
