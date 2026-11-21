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

	// node:20-alpine's node user has UID 1000; dcx updates it to the host's.
	env := nodeEnv(t)
	workspace := env.Dir
	assert.Contains(t, env.UpOutput, "Devcontainer started successfully")

	// Check the user's UID inside the container
	t.Run("uid_matches_host", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "id", "-u")
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
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/workspace/"+testFile)
		require.NoError(t, err, "should be able to read workspace files")
		assert.Contains(t, stdout, testContent)

		// Write a file from inside the container
		_, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "touch", "/workspace/created-inside.txt")
		require.NoError(t, err, "should be able to create files in workspace")

		// Verify the file exists on host
		_, err = os.Stat(workspace + "/created-inside.txt")
		assert.NoError(t, err, "file created inside container should exist on host")
	})
}
