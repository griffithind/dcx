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

func inspect(t *testing.T, container, format string) string {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "--format", format, container).CombinedOutput()
	require.NoError(t, err, "docker inspect: %s", out)
	return strings.TrimSpace(string(out))
}

// TestComposeRuntimeParityE2E: runtime settings that used to be dropped
// for compose (capAdd, securityOpt, init, typed mounts) now apply, and
// containerEnv values reach the container verbatim instead of being
// interpolated a second time by compose.
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
		"mounts": [{"type": "volume", "source": %q, "target": "/data"}]
	}`, name, volume)
	composeYAML := `services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`
	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, composeYAML)
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
}

// TestComposeNonDefaultFileStopDownE2E: stop, start and down work for a
// compose file with a non-default name even when an unrelated
// docker-compose.yml sits in the config directory. dcx now passes the -f
// files compose recorded at creation instead of relying on compose's
// default-file discovery there.
func TestComposeNonDefaultFileStopDownE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "stack.dev.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`, helpers.UniqueTestName(t))
	decoy := `services:
  decoy:
    image: alpine:latest
    command: sleep infinity
`
	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, decoy)
	stack := `services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".devcontainer", "stack.dev.yml"), []byte(stack), 0o644))
	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Equal(t, "running", helpers.GetContainerState(t, workspace))

	helpers.RunDCXInDirSuccess(t, workspace, "stop")
	assert.Equal(t, "created", helpers.GetContainerState(t, workspace))

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Equal(t, "running", helpers.GetContainerState(t, workspace))

	helpers.RunDCXInDirSuccess(t, workspace, "down")
	assert.Equal(t, "absent", helpers.GetContainerState(t, workspace))
}

// TestKeepAliveStopsPromptlyE2E: the overrideCommand keep-alive handles
// SIGTERM, so stopping does not wait out Docker's 10s kill timeout. A
// container killed after the timeout exits 137; a handled SIGTERM exits 0.
func TestKeepAliveStopsPromptlyE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := helpers.CreateTempWorkspace(t, helpers.SimpleImageConfig(t, "alpine:latest"))
	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	name := helpers.ContainerName(t, workspace)
	helpers.RunDCXInDirSuccess(t, workspace, "stop")
	assert.Equal(t, "created", helpers.GetContainerState(t, workspace))
	assert.Equal(t, "0", inspect(t, name, "{{.State.ExitCode}}"), "the keep-alive must exit on SIGTERM, not be killed (137)")
}
