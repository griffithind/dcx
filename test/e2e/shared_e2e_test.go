//go:build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/require"
)

// The environments read-only tests share (see helpers.SharedWorkspace).
// A test that changes its environment (stop, down, restart, --recreate,
// config edits) or counts processes in it needs its own instead.

// alpineEnv is a plain alpine container: helpers.SimpleImageConfig's
// settings (the workspace at /workspace), running as root, with the
// userEnvProbe off (so every test using it also checks sessions work
// without a probed environment).
func alpineEnv(t *testing.T) *helpers.Shared {
	t.Helper()
	return helpers.SharedWorkspace(t, "alpine", map[string]string{
		".devcontainer/devcontainer.json": `{
			"name": "{{name}}",
			"image": "alpine:3.24",
			"workspaceFolder": "/workspace",
			"workspaceMount": "type=bind,source=${localWorkspaceFolder},target=/workspace",
			"userEnvProbe": "none"
		}`,
	})
}

// sshClientOnce installs openssh-client in alpineEnv once: tests installing
// it at the same time would trip over apk's lock.
var (
	sshClientOnce sync.Once
	sshClientErr  error
)

// requireSSHClient makes sure alpineEnv has openssh-client (ssh-add).
func requireSSHClient(t *testing.T, workspace string) {
	t.Helper()
	sshClientOnce.Do(func() {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "apk", "add", "--no-cache", "openssh-client")
		if err != nil {
			sshClientErr = fmt.Errorf("install openssh-client: %w: %s", err, stderr)
		}
	})
	require.NoError(t, sshClientErr)
}

// richEnv is an alpine container with most of what a config can set that
// does not change the container: containerEnv and remoteEnv (overriding
// and ${containerEnv:...}), runtime secrets, forwardPorts and appPort,
// create hooks, and hostRequirements it cannot meet (they only warn; a
// GPU too when Docker has no GPU runtime).
func richEnv(t *testing.T) *helpers.Shared {
	t.Helper()
	richOnce.Do(func() {
		reqs := `"cpus": 10000, "memory": "10000gb", "storage": "10gb"`
		if out, err := exec.Command("docker", "info", "--format", "{{json .Runtimes}}").Output(); err == nil && strings.Contains(string(out), "nvidia") {
			richGPURuntime = true
		} else {
			reqs += `, "gpu": true`
		}
		richConfig = `{
			"name": "{{name}}",
			"image": "alpine:3.24",
			"workspaceFolder": "/workspace",
			"containerEnv": {
				"MY_TEST_VAR": "exec-test-value",
				"CONTAINER_VAR": "container-value",
				"SHARED_VAR": "from-container",
				"BASE": "base-value"
			},
			"remoteEnv": {
				"REMOTE_VAR_1": "remote-value-1",
				"REMOTE_VAR_2": "remote-value-2",
				"EDITOR": "vim",
				"REMOTE_VAR": "remote-value",
				"SHARED_VAR": "from-remote",
				"FROM_REMOTE": "remote-${containerEnv:BASE}"
			},
			"customizations": {"dcx": {"secrets": {
				"SECRET_ONE": "echo value-one",
				"SECRET_TWO": "echo value-two"
			}}},
			"forwardPorts": [8080, "9000:9000"],
			"appPort": [7071],
			"onCreateCommand": "echo 'onCreate executed'",
			"updateContentCommand": "echo 'updateContent executed'",
			"postCreateCommand": "echo 'postCreate executed'",
			"hostRequirements": {` + reqs + `}
		}`
	})
	return helpers.SharedWorkspace(t, "rich", map[string]string{".devcontainer/devcontainer.json": richConfig})
}

var (
	richOnce       sync.Once
	richConfig     string
	richGPURuntime bool // the Docker host has a GPU runtime (no GPU requirement then)
)

// nodeEnv runs as a non-root remoteUser (node) whose UID dcx updates to the
// host user's, with the workspace at /workspace.
func nodeEnv(t *testing.T) *helpers.Shared {
	t.Helper()
	return helpers.SharedWorkspace(t, "node", map[string]string{
		".devcontainer/devcontainer.json": `{
			"name": "{{name}}",
			"image": "node:20-alpine",
			"remoteUser": "node",
			"workspaceMount": "type=bind,source=${localWorkspaceFolder},target=/workspace",
			"workspaceFolder": "/workspace"
		}`,
	})
}

// debianEnv uses every lifecycle command form (see
// TestLifecycleCommandFormatsE2E) on debian with the default userEnvProbe;
// postCreateCommand also records the probed environment.
func debianEnv(t *testing.T) *helpers.Shared {
	t.Helper()
	rendezvous := func(name string) string {
		return `mkdir -p /tmp/rv && touch /tmp/rv/` + name + ` && i=0; while [ "$(ls /tmp/rv | wc -l)" -lt 3 ]; do i=$((i+1)); [ $i -gt 200 ] && exit 1; sleep 0.1; done; touch /tmp/saw-all-` + name + ` && echo ` + name + ` > /tmp/` + name + `.txt`
	}
	return helpers.SharedWorkspace(t, "debian", map[string]string{
		".devcontainer/devcontainer.json": fmt.Sprintf(`{
			"name": "{{name}}",
			"image": "debian:bookworm-slim",
			"workspaceFolder": "/workspace",
			"userEnvProbe": "loginInteractiveShell",
			"initializeCommand": {
				"init1": "touch .init1-marker",
				"init2": "touch .init2-marker"
			},
			"onCreateCommand": {
				"setup1": "echo oncreate1 > /tmp/oncreate1.txt",
				"setup2": "echo oncreate2 > /tmp/oncreate2.txt"
			},
			"updateContentCommand": "echo string-format > /tmp/updatecontent.txt",
			"postCreateCommand": {
				"cmd1": %q,
				"cmd2": %q,
				"cmd3": %q,
				"array": ["sh", "-c", "echo array-in-object > /tmp/array.txt"],
				"probe": "printenv > /tmp/probe-result.txt"
			},
			"postStartCommand": ["sh", "-c", "echo array-format > /tmp/poststart.txt"]
		}`, rendezvous("cmd1"), rendezvous("cmd2"), rendezvous("cmd3")),
	})
}

// composeEnv is a compose project with two services (app, the primary, and
// db) and runServices naming both.
func composeEnv(t *testing.T) *helpers.Shared {
	t.Helper()
	helpers.RequireComposeAvailable(t)
	return helpers.SharedWorkspace(t, "compose", map[string]string{
		".devcontainer/devcontainer.json": `{
			"name": "{{name}}",
			"dockerComposeFile": "docker-compose.yml",
			"service": "app",
			"runServices": ["app", "db"],
			"workspaceFolder": "/workspace"
		}`,
		".devcontainer/docker-compose.yml": `services:
  app:
    image: alpine:3.24
    command: sleep infinity
    init: true
    volumes:
      - ..:/workspace:cached
    depends_on:
      - db
  db:
    image: alpine:3.24
    command: sleep infinity
    init: true
`,
	})
}
