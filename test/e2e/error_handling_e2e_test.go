//go:build e2e

package e2e

import (
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestErrorMissingDockerfileE2E checks that a missing Dockerfile fails up
// and build with docker's message naming it. The other bad-config errors
// are unit tests (internal/cli/errors_test.go); this one comes from docker
// build itself.
func TestErrorMissingDockerfileE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := helpers.CreateTempWorkspace(t, `{
		"name": "Missing Dockerfile Test",
		"build": {"dockerfile": "NonexistentDockerfile"},
		"workspaceFolder": "/workspace"
	}`)
	for _, command := range []string{"up", "build"} {
		t.Run(command, func(t *testing.T) {
			stdout, stderr, err := helpers.RunDCXInDir(t, workspace, command)
			require.Error(t, err)
			assert.Contains(t, stdout+stderr, "NonexistentDockerfile")
		})
	}
}
