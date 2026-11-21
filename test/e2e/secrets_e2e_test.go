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

// The secret plumbing (names, modes, owners, the tmpfs size check) is unit
// tested in internal/container; these tests check it end to end (the
// Dockerfile plan in TestDockerfileLifecycleE2E).

// sshOut runs a command in the workspace over dcx ssh and returns its
// trimmed output, failing the test on error.
func sshOut(t *testing.T, ws string, argv ...string) string {
	t.Helper()
	stdout, stderr, err := helpers.RunDCXInDir(t, ws, append([]string{"ssh", "--"}, argv...)...)
	require.NoError(t, err, "%v: %s", argv, stderr)
	return strings.TrimSpace(helpers.StripANSI(stdout))
}

// TestRuntimeSecretsImage: runtime secrets are files on the /run/secrets
// tmpfs, 0400, owned by the (root) session user.
func TestRuntimeSecretsImage(t *testing.T) {
	t.Parallel()
	env := richEnv(t)
	ws := env.Dir
	assert.Contains(t, env.UpOutput, "Fetching runtime secrets")
	assert.Equal(t, "value-one", sshOut(t, ws, "cat", "/run/secrets/SECRET_ONE"))
	assert.Equal(t, "value-two", sshOut(t, ws, "cat", "/run/secrets/SECRET_TWO"))
	assert.Contains(t, sshOut(t, ws, "df", "-T", "/run/secrets"), "tmpfs", "secrets are never written to disk")
	assert.Equal(t, "400 root", sshOut(t, ws, "stat", "-c", "%a %U", "/run/secrets/SECRET_ONE"))
}

// TestRuntimeSecretsNonRootUser: a non-root remote user owns and can read
// its secrets.
func TestRuntimeSecretsNonRootUser(t *testing.T) {
	t.Parallel()
	ws := nodeEnv(t).Dir
	assert.Equal(t, "node", sshOut(t, ws, "whoami"))
	assert.Equal(t, "non-root-secret-value", sshOut(t, ws, "cat", "/run/secrets/USER_SECRET"))
	assert.Equal(t, "400 node", sshOut(t, ws, "stat", "-c", "%a %U", "/run/secrets/USER_SECRET"))
}

// TestSecretsCompose: build secrets reach a compose service's build, and
// runtime secrets belong to its non-root remote user.
func TestSecretsCompose(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	ws := helpers.CreateWorkspaceFiles(t, map[string]string{
		".devcontainer/Dockerfile": `FROM node:20-slim
RUN --mount=type=secret,id=COMPOSE_BUILD_SECRET cat /run/secrets/COMPOSE_BUILD_SECRET > /compose-build-secret-result
`,
		".devcontainer/docker-compose.yml": `services:
  app:
    build:
      context: .
      dockerfile: Dockerfile
    command: sleep infinity
    init: true
    volumes:
      - ..:/workspace:cached
`,
		".devcontainer/devcontainer.json": fmt.Sprintf(`{
			"name": %q,
			"dockerComposeFile": "docker-compose.yml",
			"service": "app",
			"remoteUser": "node",
			"workspaceFolder": "/workspace",
			"customizations": {"dcx": {
				"secrets": {"COMPOSE_USER_SECRET": "echo compose-user-secret"},
				"buildSecrets": {"COMPOSE_BUILD_SECRET": "echo compose-secret-value"}
			}}
		}`, helpers.UniqueTestName(t)),
	})
	assert.Contains(t, helpers.RunDCXInDirSuccess(t, ws, "up"), "Fetching build secrets")

	assert.Equal(t, "compose-secret-value", sshOut(t, ws, "cat", "/compose-build-secret-result"))
	assert.Equal(t, "node", sshOut(t, ws, "whoami"))
	assert.Equal(t, "compose-user-secret", sshOut(t, ws, "cat", "/run/secrets/COMPOSE_USER_SECRET"))
	assert.Equal(t, "400 node", sshOut(t, ws, "stat", "-c", "%a %U", "/run/secrets/COMPOSE_USER_SECRET"))
}
