//go:build e2e

package e2e

import (
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
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/task1.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "task1", "task1 should have executed")

	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/task2.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "task2", "task2 should have executed")

	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/task3.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "task3", "task3 should have executed")
}

// TestParallelPostCreateCommandTimingE2E verifies commands actually run in parallel
// by checking that total execution time is less than sequential would require.
func TestParallelPostCreateCommandTimingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Each command sleeps 1 second. If parallel, total time < 3s. If sequential, ~3s.
	// We use timestamps to verify parallel execution.
	devcontainerJSON := `{
		"name": "Parallel Timing Test",
		"image": "debian:bookworm-slim",
		"workspaceFolder": "/workspace",
		"postCreateCommand": {
			"cmd1": "date +%s > /tmp/start1.txt && sleep 1 && date +%s > /tmp/end1.txt",
			"cmd2": "date +%s > /tmp/start2.txt && sleep 1 && date +%s > /tmp/end2.txt",
			"cmd3": "date +%s > /tmp/start3.txt && sleep 1 && date +%s > /tmp/end3.txt"
		}
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// All commands should have completed
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c",
		"cat /tmp/end1.txt /tmp/end2.txt /tmp/end3.txt | wc -l")
	require.NoError(t, err)
	assert.Contains(t, stdout, "3", "all three commands should have completed")

	// Check that start times overlap (parallel execution)
	// Get all start times and verify they're within 1 second of each other
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", `
		start1=$(cat /tmp/start1.txt)
		start2=$(cat /tmp/start2.txt)
		start3=$(cat /tmp/start3.txt)
		# Find min and max start times
		min=$start1
		max=$start1
		for s in $start2 $start3; do
			[ $s -lt $min ] && min=$s
			[ $s -gt $max ] && max=$s
		done
		diff=$((max - min))
		echo "start_diff=$diff"
		# If parallel, all should start within 1 second
		[ $diff -le 1 ] && echo "PARALLEL" || echo "SEQUENTIAL"
	`)
	require.NoError(t, err)
	assert.Contains(t, stdout, "PARALLEL", "commands should have started in parallel (within 1 second of each other)")
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
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/oncreate1.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "oncreate1", "onCreateCommand task1 should have executed")

	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/oncreate2.txt")
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
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/oncreate.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "string-format")

	// Verify parallel object format worked
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/parallel1.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "parallel1")

	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/parallel2.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "parallel2")

	// Verify array format worked
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/poststart.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "array-format")
}
