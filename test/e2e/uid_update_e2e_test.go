//go:build e2e

package e2e

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUIDUpdateE2E tests that the user UID is updated to match the host UID.
func TestUIDUpdateE2E(t *testing.T) {
	// Skip on Windows - UID update not supported
	if runtime.GOOS == "windows" {
		t.Skip("UID update not supported on Windows")
	}

	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Get host UID
	hostUID := os.Getuid()
	if hostUID == 0 {
		t.Skip("Skipping UID update test when running as root")
	}

	// Create config with remoteUser that has a different UID
	// node:20-alpine has a 'node' user with UID 1000
	devcontainerJSON := `{
		"name": "` + helpers.UniqueTestName(t) + `",
		"image": "node:20-alpine",
		"remoteUser": "node",
		"workspaceFolder": "/workspace"
	}`

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Contains(t, stdout, "Devcontainer started successfully")

	// Check the user's UID inside the container
	t.Run("uid_matches_host", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "id", "-u")
		require.NoError(t, err, "failed to get UID inside container")

		containerUID := strings.TrimSpace(stdout)
		expectedUID := strconv.Itoa(hostUID)

		assert.Equal(t, expectedUID, containerUID,
			"Container user UID (%s) should match host UID (%s)", containerUID, expectedUID)
	})

	// Verify the user can access workspace files without permission issues
	t.Run("workspace_access", func(t *testing.T) {
		// Create a test file in workspace
		testFile := "test-uid-access.txt"
		testContent := "uid-test-content"
		err := os.WriteFile(workspace+"/"+testFile, []byte(testContent), 0644)
		require.NoError(t, err)

		// Read it from inside the container
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/workspace/"+testFile)
		require.NoError(t, err, "should be able to read workspace files")
		assert.Contains(t, stdout, testContent)

		// Write a file from inside the container
		_, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "touch", "/workspace/created-inside.txt")
		require.NoError(t, err, "should be able to create files in workspace")

		// Verify the file exists on host
		_, err = os.Stat(workspace + "/created-inside.txt")
		assert.NoError(t, err, "file created inside container should exist on host")
	})
}

// TestUIDUpdateDisabledE2E tests that UID update can be disabled.
func TestUIDUpdateDisabledE2E(t *testing.T) {
	// Skip on Windows - UID update not supported
	if runtime.GOOS == "windows" {
		t.Skip("UID update not supported on Windows")
	}

	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Get host UID
	hostUID := os.Getuid()
	if hostUID == 0 {
		t.Skip("Skipping UID update test when running as root")
	}

	// Create config with updateRemoteUserUID: false
	devcontainerJSON := `{
		"name": "` + helpers.UniqueTestName(t) + `",
		"image": "node:20-alpine",
		"remoteUser": "node",
		"updateRemoteUserUID": false,
		"workspaceFolder": "/workspace"
	}`

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Check the user's UID inside the container - should be original (1000)
	t.Run("uid_not_updated", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "id", "-u")
		require.NoError(t, err)

		containerUID := strings.TrimSpace(stdout)

		// When disabled, UID should stay at original value (1000 for node)
		assert.Equal(t, "1000", containerUID,
			"Container user UID should remain original (1000) when updateRemoteUserUID is false")
	})
}

// TestUIDUpdateRootUserE2E tests that root user is not updated.
func TestUIDUpdateRootUserE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create config with remoteUser: root
	devcontainerJSON := `{
		"name": "` + helpers.UniqueTestName(t) + `",
		"image": "alpine:latest",
		"remoteUser": "root",
		"workspaceFolder": "/workspace"
	}`

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Check the user's UID inside the container - should be 0 (root)
	t.Run("root_uid_not_updated", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "id", "-u")
		require.NoError(t, err)

		containerUID := strings.TrimSpace(stdout)
		assert.Equal(t, "0", containerUID, "Root user UID should remain 0")
	})
}

// TestUIDUpdateNoFeaturesE2E tests UID update works without features.
func TestUIDUpdateNoFeaturesE2E(t *testing.T) {
	// Skip on Windows
	if runtime.GOOS == "windows" {
		t.Skip("UID update not supported on Windows")
	}

	t.Parallel()
	helpers.RequireDockerAvailable(t)

	hostUID := os.Getuid()
	if hostUID == 0 {
		t.Skip("Skipping UID update test when running as root")
	}

	// Simple config without features
	devcontainerJSON := `{
		"name": "` + helpers.UniqueTestName(t) + `",
		"image": "node:20-alpine",
		"remoteUser": "node",
		"workspaceFolder": "/workspace"
	}`

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify UID is updated even without features
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "id", "-u")
	require.NoError(t, err)

	containerUID := strings.TrimSpace(stdout)
	expectedUID := strconv.Itoa(hostUID)

	assert.Equal(t, expectedUID, containerUID,
		"UID should be updated even without features")
}
