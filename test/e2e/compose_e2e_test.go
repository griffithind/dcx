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

// TestComposeWorkflowE2E walks a compose devcontainer through its
// lifecycle: up, stop, start, --recreate, a config edit that renames the
// primary service, and down.
//   - The compose file has a non-default name next to an unrelated
//     docker-compose.yml: dcx passes the -f files compose recorded at
//     creation instead of relying on compose's default-file discovery.
//   - A recreate never runs `docker compose down -v`: the named volume's
//     content survives.
//   - A recreate after the primary service was renamed does not leave the
//     old one running: compose down runs with the edited files, so the old
//     service is an orphan.
func TestComposeWorkflowE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	name := helpers.UniqueTestName(t)
	config := func(service string) string {
		return fmt.Sprintf(`{
			"name": %q,
			"dockerComposeFile": "stack.dev.yml",
			"service": %q,
			"workspaceFolder": "/workspace"
		}`, name, service)
	}
	stack := func(service string) string {
		return fmt.Sprintf(`services:
  %s:
    image: alpine:3.24
    command: sleep infinity
    init: true
    volumes:
      - ..:/workspace:cached
      - data:/data
volumes:
  data:
`, service)
	}
	decoy := `services:
  decoy:
    image: alpine:3.24
    command: sleep infinity
    init: true
`
	workspace := helpers.CreateTempComposeWorkspace(t, config("app"), decoy)
	dir := filepath.Join(workspace, ".devcontainer")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stack.dev.yml"), []byte(stack("app")), 0o644))
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down", "--remove-orphans", "--volumes") })
	state := func() string { return helpers.GetContainerState(t, workspace) }

	assert.Equal(t, "absent", state())
	assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "up"), "Devcontainer started successfully")
	assert.Equal(t, "running", state())
	dcxExec(t, workspace, "sh", "-c", "echo keep-me > /data/marker")

	assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "stop"), "stopped")
	assert.Equal(t, "created", state())
	assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "up"), "started")
	assert.Equal(t, "running", state())

	before := helpers.GetStatus(t, workspace).Container
	require.NotNil(t, before)
	helpers.RunDCXInDirSuccess(t, workspace, "up", "--recreate")
	after := helpers.GetStatus(t, workspace).Container
	require.NotNil(t, after)
	assert.NotEqual(t, before.ID, after.ID, "--recreate must replace the container")
	assert.Equal(t, "keep-me", dcxExec(t, workspace, "cat", "/data/marker"), "named volume content lost after --recreate")

	project := helpers.GetStatus(t, workspace).ResourceName
	require.NoError(t, os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(config("dev")), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stack.dev.yml"), []byte(stack("dev")), 0o644))
	assert.Equal(t, "stale", state())
	helpers.RunDCXInDirSuccess(t, workspace, "up")
	out, err := exec.Command("docker", "ps", "-a", "--format", "{{.Label \"com.docker.compose.service\"}}",
		"--filter", "label=com.docker.compose.project="+project).Output()
	require.NoError(t, err)
	assert.Equal(t, "dev", strings.TrimSpace(string(out)), "only the renamed service remains")
	assert.Equal(t, "keep-me", dcxExec(t, workspace, "cat", "/data/marker"), "named volume content lost after a config change")

	assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "down", "--volumes"), "removed")
	assert.Equal(t, "absent", state())
}

// TestComposeMultiServiceE2E: a project whose primary service depends on
// another starts both and takes commands; a second up changes nothing.
func TestComposeMultiServiceE2E(t *testing.T) {
	t.Parallel()
	env := composeEnv(t)
	assert.Contains(t, env.UpOutput, "Devcontainer started successfully")
	assert.Equal(t, "running", helpers.GetContainerState(t, env.Dir))
	stdout, _, err := helpers.RunDCXInDir(t, env.Dir, "ssh", "--", "echo", "test")
	require.NoError(t, err)
	assert.Contains(t, stdout, "test")

	assert.Contains(t, helpers.RunDCXInDirSuccess(t, env.Dir, "up"), "already running", "up is idempotent")
	assert.Equal(t, "running", helpers.GetContainerState(t, env.Dir))
}

// TestComposeContainerLabelsE2E: the primary compose container carries
// dcx's labels.
func TestComposeContainerLabelsE2E(t *testing.T) {
	t.Parallel()
	env := composeEnv(t)
	containerName := helpers.ContainerName(t, env.Dir)
	label := func(name string) string {
		out, err := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.`+name+`"}}`, containerName).CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", out)
		return strings.TrimSpace(string(out))
	}
	assert.Equal(t, env.Dir, label("workspace.path"))
	assert.Equal(t, "compose", label("build.method"))
	assert.NotEmpty(t, label("compose.project"))
	assert.Equal(t, "app", label("compose.service"))
	assert.Equal(t, "true", label("container.primary"))
	assert.Equal(t, "true", label("managed"))
}

// TestComposeFeaturesE2E builds a local and a registry feature onto a
// compose service: up writes nothing into .devcontainer, a second up keeps
// the container, and up after down creates a new container from the cached
// features image. Hooks get the PATH the userEnvProbe captured.
func TestComposeFeaturesE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	workspace := helpers.CreateWorkspaceFiles(t, map[string]string{
		".devcontainer/devcontainer.json": fmt.Sprintf(`{
			"name": %q,
			"dockerComposeFile": "docker-compose.yml",
			"service": "app",
			"workspaceFolder": "/workspace",
			"features": {
				"./features/simple-marker": {},
				"ghcr.io/devcontainers/features/common-utils:2": {
					"installZsh": false,
					"installOhMyZsh": false,
					"configureZshAsDefaultShell": false,
					"upgradePackages": false
				}
			},
			"userEnvProbe": "loginShell",
			"postCreateCommand": "echo PATH=$PATH > /tmp/path-check.txt"
		}`, helpers.UniqueTestName(t)),
		".devcontainer/docker-compose.yml": `services:
  app:
    image: debian:bookworm-slim
    command: sleep infinity
    init: true
    volumes:
      - ..:/workspace:cached
`,
		".devcontainer/features/simple-marker/devcontainer-feature.json": `{"id": "simple-marker", "version": "1.0.0", "name": "Simple Marker Feature"}`,
		".devcontainer/features/simple-marker/install.sh":                "#!/bin/sh\nset -e\necho \"feature installed\" > /tmp/feature-marker\n",
	})
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	filesBefore := listFilesInDir(t, devcontainerDir)

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Equal(t, filesBefore, listFilesInDir(t, devcontainerDir), "dcx up should not create files in .devcontainer")
	assert.NoFileExists(t, filepath.Join(devcontainerDir, "Dockerfile.dcx-features"))
	assert.NoDirExists(t, filepath.Join(devcontainerDir, "feature_0"))
	containerID := helpers.ContainerID(t, workspace)
	imageID := helpers.ContainerImageID(t, workspace)
	assert.Equal(t, "feature installed", dcxExec(t, workspace, "cat", "/tmp/feature-marker"))
	assert.Contains(t, dcxExec(t, workspace, "cat", "/tmp/path-check.txt"), "PATH=/")

	assert.Contains(t, helpers.RunDCXInDirSuccess(t, workspace, "up"), "already running")
	assert.Equal(t, containerID, helpers.ContainerID(t, workspace), "a second up keeps the container")

	helpers.RunDCXInDirSuccess(t, workspace, "down")
	assert.Equal(t, "absent", helpers.GetContainerState(t, workspace))
	helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.NotEqual(t, containerID, helpers.ContainerID(t, workspace), "a new container")
	assert.Equal(t, imageID, helpers.ContainerImageID(t, workspace), "from the cached image")
	assert.Equal(t, "feature installed", dcxExec(t, workspace, "cat", "/tmp/feature-marker"))
}
