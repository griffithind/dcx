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

// recordingFeature is a local feature whose install.sh writes what it sees
// to /opt/rec/<name>.
func recordingFeature(name, metadata, record string) map[string]string {
	return map[string]string{
		".devcontainer/" + name + "/devcontainer-feature.json": metadata,
		".devcontainer/" + name + "/install.sh":                "#!/bin/sh\nset -e\nmkdir -p /opt/rec\necho \"" + record + "\" > /opt/rec/" + name + "\n",
	}
}

// TestFeatureInstallEnvironmentE2E: features install for the image's USER
// when there is no containerUser (F3), a feature's containerEnv is set
// before its install.sh runs (F4), and its options reach only its own
// install.sh (F5); installsAfter orders the two.
func TestFeatureInstallEnvironmentE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	files := map[string]string{
		".devcontainer/devcontainer.json": fmt.Sprintf(`{
			"name": %q,
			"build": {"dockerfile": "Dockerfile"},
			"features": {"./first": {"greeting": "hi there"}, "./second": {}}
		}`, helpers.UniqueTestName(t)),
		".devcontainer/Dockerfile": "FROM alpine:3.24\nRUN adduser -D app\nUSER app\n",
	}
	for k, v := range recordingFeature("first",
		`{"id": "first", "options": {"greeting": {"type": "string", "default": "hello"}}, "containerEnv": {"FIRST_HOME": "/opt/first"}}`,
		`$_CONTAINER_USER|$_REMOTE_USER|$GREETING|$FIRST_HOME`) {
		files[k] = v
	}
	for k, v := range recordingFeature("second", `{"id": "second", "installsAfter": ["./first"]}`, `${GREETING:-unset}|$FIRST_HOME`) {
		files[k] = v
	}
	ws := helpers.CreateWorkspaceFiles(t, files)

	helpers.RunDCXInDirSuccess(t, ws, "up")
	stdout, stderr, err := helpers.RunDCXInDir(t, ws, "ssh", "--", "cat", "/opt/rec/first", "/opt/rec/second")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, []string{"app|app|hi there|/opt/first", "unset|/opt/first"}, strings.Split(strings.TrimSpace(stdout), "\n"),
		"first sees the image user, its option and its containerEnv; second sees no option of first")
}

// TestFeaturesInstallForTheComposeUserE2E: without a containerUser,
// features install for the compose service's user (F3).
func TestFeaturesInstallForTheComposeUserE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	files := map[string]string{
		".devcontainer/devcontainer.json": fmt.Sprintf(`{
			"name": %q,
			"dockerComposeFile": "docker-compose.yml",
			"service": "app",
			"workspaceFolder": "/workspace",
			"features": {"./rec": {}}
		}`, helpers.UniqueTestName(t)),
		".devcontainer/docker-compose.yml": "services:\n  app:\n    image: alpine:3.24\n    command: sleep infinity\n    user: nobody\n",
	}
	for k, v := range recordingFeature("rec", `{"id": "rec"}`, `$_CONTAINER_USER|$_REMOTE_USER`) {
		files[k] = v
	}
	ws := helpers.CreateWorkspaceFiles(t, files)

	helpers.RunDCXInDirSuccess(t, ws, "up")
	stdout, stderr, err := helpers.RunDCXInDir(t, ws, "ssh", "--", "cat", "/opt/rec/rec")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, "nobody|nobody", strings.TrimSpace(stdout))
}
