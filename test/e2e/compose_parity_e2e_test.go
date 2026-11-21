//go:build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func inspect(t *testing.T, container, format string) string {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "--format", format, container).CombinedOutput()
	require.NoError(t, err, "docker inspect: %s", out)
	return strings.TrimSpace(string(out))
}

// TestComposeRuntimeParityE2E: runtime settings that used to be dropped
// for compose (capAdd, securityOpt, init, typed mounts) now apply, and
// containerEnv values reach the container verbatim instead of being
// interpolated a second time by compose. The primary shares db's network
// namespace (network_mode: service:db): the agent's port must be published
// on db, since Docker rejects publishing it on app. Without a
// containerUser, features install for the compose service's user (F3).
func TestComposeRuntimeParityE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	name := helpers.UniqueTestName(t)
	volume := name + "-data"
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"capAdd": ["SYS_PTRACE"],
		"securityOpt": ["no-new-privileges:true"],
		"init": true,
		"containerEnv": {"PRICE": "costs $DCX_E2E_UNSET-5"},
		"mounts": [{"type": "volume", "source": %q, "target": "/data"}],
		"features": {"./rec": {}}
	}`, name, volume)
	composeYAML := `services:
  app:
    image: alpine:3.24
    command: sleep infinity
    user: nobody
    network_mode: service:db
    volumes:
      - ..:/workspace:cached
  db:
    image: alpine:3.24
    command: sleep infinity
    init: true
`
	files := map[string]string{
		".devcontainer/devcontainer.json":  devcontainerJSON,
		".devcontainer/docker-compose.yml": composeYAML,
	}
	for k, v := range recordingFeature("rec", `{"id": "rec"}`, `$_CONTAINER_USER|$_REMOTE_USER`) {
		files[k] = v
	}
	workspace := helpers.CreateWorkspaceFiles(t, files)
	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
		_ = exec.Command("docker", "volume", "rm", "-f", volume).Run()
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	c := helpers.ContainerName(t, workspace)

	assert.Contains(t, inspect(t, c, "{{.HostConfig.CapAdd}}"), "SYS_PTRACE")
	assert.Contains(t, inspect(t, c, "{{.HostConfig.SecurityOpt}}"), "no-new-privileges:true")
	assert.Equal(t, "true", inspect(t, c, "{{.HostConfig.Init}}"))

	stdout := helpers.RunDCXInDirSuccess(t, workspace, "ssh", "--", "printenv", "PRICE")
	assert.Equal(t, "costs $DCX_E2E_UNSET-5", strings.TrimSpace(stdout))

	// The named volume keeps its own name (as with docker run), rather than
	// being prefixed with the compose project.
	mounts := inspect(t, c, `{{range .Mounts}}{{.Type}}:{{.Name}}:{{.Destination}} {{end}}`)
	assert.Contains(t, mounts, "volume:"+volume+":/data")

	assert.Equal(t, "nobody|nobody", dcxExec(t, workspace, "cat", "/opt/rec/rec"))
}
