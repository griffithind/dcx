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
// tested in internal/container; these tests check it end to end, one
// container per plan type.

// sshOut runs a command in the workspace over dcx ssh and returns its
// trimmed output, failing the test on error.
func sshOut(t *testing.T, ws string, argv ...string) string {
	t.Helper()
	stdout, stderr, err := helpers.RunDCXInDir(t, ws, append([]string{"ssh", "--"}, argv...)...)
	require.NoError(t, err, "%v: %s", argv, stderr)
	return strings.TrimSpace(helpers.StripANSI(stdout))
}

// TestRuntimeSecretsImage: several secrets on the /run/secrets tmpfs,
// mode 0400, owned by the (root) remote user.
func TestRuntimeSecretsImage(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"customizations": {"dcx": {"secrets": {
			"SECRET_ONE": "echo value-one",
			"SECRET_TWO": "echo value-two"
		}}}
	}`, helpers.UniqueTestName(t)))
	assert.Contains(t, helpers.RunDCXInDirSuccess(t, ws, "up"), "Mounting secrets")

	assert.Equal(t, "value-one", sshOut(t, ws, "cat", "/run/secrets/SECRET_ONE"))
	assert.Equal(t, "value-two", sshOut(t, ws, "cat", "/run/secrets/SECRET_TWO"))
	assert.Contains(t, sshOut(t, ws, "df", "-T", "/run/secrets"), "tmpfs", "secrets are never written to disk")
	assert.Equal(t, "400 root", sshOut(t, ws, "stat", "-c", "%a %U", "/run/secrets/SECRET_ONE"))
}

// TestRuntimeSecretsNonRootUser: a non-root remote user owns and can read
// its secrets.
func TestRuntimeSecretsNonRootUser(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "node:20-slim",
		"remoteUser": "node",
		"workspaceFolder": "/workspace",
		"customizations": {"dcx": {"secrets": {"USER_SECRET": "echo non-root-secret-value"}}}
	}`, helpers.UniqueTestName(t)))
	helpers.RunDCXInDirSuccess(t, ws, "up")

	assert.Equal(t, "node", sshOut(t, ws, "whoami"))
	assert.Equal(t, "non-root-secret-value", sshOut(t, ws, "cat", "/run/secrets/USER_SECRET"))
	assert.Equal(t, "400 node", sshOut(t, ws, "stat", "-c", "%a %U", "/run/secrets/USER_SECRET"))
}

// TestSecretsDockerfile: a build secret is available to RUN --mount and
// nowhere else; a runtime secret is mounted alongside.
func TestSecretsDockerfile(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateWorkspaceFiles(t, map[string]string{
		".devcontainer/Dockerfile": `FROM alpine:3.24
RUN --mount=type=secret,id=BUILD_ONLY cat /run/secrets/BUILD_ONLY > /build-time-value
RUN --mount=type=secret,id=SENSITIVE echo "secret was accessed" > /build-status
`,
		".devcontainer/devcontainer.json": fmt.Sprintf(`{
			"name": %q,
			"build": {"dockerfile": "Dockerfile"},
			"workspaceFolder": "/workspace",
			"customizations": {"dcx": {
				"secrets": {"RUNTIME_SECRET": "echo runtime-value"},
				"buildSecrets": {
					"BUILD_ONLY": "echo build-time-only",
					"SENSITIVE": "echo super-secret-do-not-persist"
				}
			}}
		}`, helpers.UniqueTestName(t)),
	})
	stdout := helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Contains(t, stdout, "Fetching build secrets")
	assert.Contains(t, stdout, "Mounting secrets")

	assert.Equal(t, "build-time-only", sshOut(t, ws, "cat", "/build-time-value"))
	assert.Equal(t, "secret was accessed", sshOut(t, ws, "cat", "/build-status"))
	assert.Equal(t, "runtime-value", sshOut(t, ws, "cat", "/run/secrets/RUNTIME_SECRET"))
	for _, name := range []string{"BUILD_ONLY", "SENSITIVE"} {
		_, _, err := helpers.RunDCXInDir(t, ws, "ssh", "--", "test", "-e", "/run/secrets/"+name)
		assert.Error(t, err, "build secret %s must not exist at runtime", name)
	}
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
