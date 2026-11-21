//go:build e2e

package e2e

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

// The environments read-only tests share (see helpers.SharedWorkspace).
// A test that changes its environment (stop, down, restart, --recreate,
// config edits) or counts processes in it needs its own instead.

// sharedFiles lists every shared environment's files by key.
var sharedFiles = map[string]func() (map[string]string, error){
	"alpine":  alpineFiles,
	"rich":    richFiles,
	"node":    nodeFiles,
	"debian":  debianFiles,
	"compose": composeFiles,
	"devuser": devUserFiles,
}

var warmOnce sync.Once

// sharedEnv returns the shared environment key. The first call starts every
// shared environment in the background: tests waiting for one hold a
// parallel slot, so bringing them up one after another as tests first ask
// for them left the build-heavy tests queued behind the waits.
func sharedEnv(t *testing.T, key string) *helpers.Shared {
	t.Helper()
	warmOnce.Do(func() {
		composeOK := exec.Command("docker", "compose", "version").Run() == nil
		for k, files := range sharedFiles {
			if k == "compose" && !composeOK {
				continue
			}
			if f, err := files(); err == nil {
				helpers.StartShared(t, k, f)
			}
		}
	})
	files, err := sharedFiles[key]()
	require.NoError(t, err)
	return helpers.SharedWorkspace(t, key, files)
}

// alpineEnv is a plain alpine container: helpers.SimpleImageConfig's
// settings (the workspace at /workspace), running as root, with the
// userEnvProbe off (so every test using it also checks sessions work
// without a probed environment).
func alpineEnv(t *testing.T) *helpers.Shared { return sharedEnv(t, "alpine") }

func alpineFiles() (map[string]string, error) {
	return map[string]string{
		".devcontainer/devcontainer.json": `{
			"name": "{{name}}",
			"image": "alpine:3.24",
			"workspaceFolder": "/workspace",
			"workspaceMount": "type=bind,source=${localWorkspaceFolder},target=/workspace",
			"userEnvProbe": "none"
		}`,
	}, nil
}

// alpineToolsOnce installs the packages tests need in alpineEnv, once:
// tests running apk at the same time trip over its lock.
var (
	alpineToolsOnce sync.Once
	alpineToolsErr  error
)

// requireAlpineTools makes sure alpineEnv has openssh-client (ssh-add) and
// file.
func requireAlpineTools(t *testing.T, workspace string) {
	t.Helper()
	alpineToolsOnce.Do(func() {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "apk", "add", "--no-cache", "openssh-client", "file")
		if err != nil {
			alpineToolsErr = fmt.Errorf("install openssh-client and file: %w: %s", err, stderr)
		}
	})
	require.NoError(t, alpineToolsErr)
}

// richEnv is an alpine container with most of what a config can set that
// does not change the container: containerEnv and remoteEnv (overriding
// and ${containerEnv:...}), runtime secrets, forwardPorts and appPort (a
// number and a range),
// create hooks, and hostRequirements it cannot meet (they only warn; a
// GPU too when Docker has no GPU runtime).
func richEnv(t *testing.T) *helpers.Shared { return sharedEnv(t, "rich") }

func richFiles() (map[string]string, error) {
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
			"appPort": [7071, "127.0.0.1::8000-8002"],
			"onCreateCommand": "echo 'onCreate executed'",
			"updateContentCommand": "echo 'updateContent executed'",
			"postCreateCommand": "echo 'postCreate executed'",
			"hostRequirements": {` + reqs + `}
		}`
	})
	return map[string]string{".devcontainer/devcontainer.json": richConfig}, nil
}

var (
	richOnce       sync.Once
	richConfig     string
	richGPURuntime bool // the Docker host has a GPU runtime (no GPU requirement then)
)

// nodeEnv runs as a non-root remoteUser (node) whose UID dcx updates to the
// host user's, with the workspace at /workspace and a runtime secret.
func nodeEnv(t *testing.T) *helpers.Shared { return sharedEnv(t, "node") }

func nodeFiles() (map[string]string, error) {
	return map[string]string{
		".devcontainer/devcontainer.json": `{
			"name": "{{name}}",
			"image": "node:20-alpine",
			"remoteUser": "node",
			"workspaceMount": "type=bind,source=${localWorkspaceFolder},target=/workspace",
			"workspaceFolder": "/workspace",
			"customizations": {"dcx": {"secrets": {"USER_SECRET": "echo non-root-secret-value"}}}
		}`,
	}, nil
}

// debianEnv uses every lifecycle command form (see
// TestLifecycleCommandFormatsE2E) on debian with the default userEnvProbe;
// postCreateCommand also records the probed environment.
func debianEnv(t *testing.T) *helpers.Shared { return sharedEnv(t, "debian") }

func debianFiles() (map[string]string, error) {
	rendezvous := func(name string) string {
		return `mkdir -p /tmp/rv && touch /tmp/rv/` + name + ` && i=0; while [ "$(ls /tmp/rv | wc -l)" -lt 3 ]; do i=$((i+1)); [ $i -gt 200 ] && exit 1; sleep 0.1; done; touch /tmp/saw-all-` + name + ` && echo ` + name + ` > /tmp/` + name + `.txt`
	}
	return map[string]string{
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
	}, nil
}

// composeEnv is a compose project with two services (app, the primary, and
// db) and runServices naming both; app publishes a port forwardPorts also
// lists.
func composeEnv(t *testing.T) *helpers.Shared {
	t.Helper()
	helpers.RequireComposeAvailable(t)
	return sharedEnv(t, "compose")
}

func composeFiles() (map[string]string, error) {
	composeOnce.Do(func() {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			composeErr = err
			return
		}
		composePort = l.Addr().(*net.TCPAddr).Port
		composeErr = l.Close()
	})
	if composeErr != nil {
		return nil, composeErr
	}
	return map[string]string{
		".devcontainer/devcontainer.json": `{
			"name": "{{name}}",
			"dockerComposeFile": "docker-compose.yml",
			"service": "app",
			"runServices": ["app", "db"],
			"workspaceFolder": "/workspace",
			"forwardPorts": [` + fmt.Sprint(composePort) + `]
		}`,
		".devcontainer/docker-compose.yml": `services:
  app:
    image: alpine:3.24
    command: sleep infinity
    init: true
    ports:
      - "127.0.0.1:` + fmt.Sprintf("%d:%d", composePort, composePort) + `"
    volumes:
      - ..:/workspace:cached
    depends_on:
      - db
  db:
    image: alpine:3.24
    command: sleep infinity
    init: true
`,
	}, nil
}

var (
	composeOnce sync.Once
	composeErr  error
	composePort int // published by the compose file and in forwardPorts
)

// devUserEnv runs sessions as a non-root remoteUser (dev, uid 1234, also in
// group extra) without the UID update; another user (other) has its own
// authorized_keys, holding otherKeyPath's public key, and is the image's
// USER (so the agent must not run as the image's USER: it could not switch
// to dev). overrideCommand is false: PID 1 is the Dockerfile's CMD.
func devUserEnv(t *testing.T) *helpers.Shared { return sharedEnv(t, "devuser") }

func devUserFiles() (map[string]string, error) {
	devUserOnce.Do(func() {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			devUserErr = err
			return
		}
		sshPub, err := gossh.NewPublicKey(pub)
		if err != nil {
			devUserErr = err
			return
		}
		block, err := gossh.MarshalPrivateKey(priv, "")
		if err != nil {
			devUserErr = err
			return
		}
		dir, err := os.MkdirTemp("", "dcx-e2e-key-")
		if err != nil {
			devUserErr = err
			return
		}
		otherKeyPath = filepath.Join(dir, "other_key")
		if err := os.WriteFile(otherKeyPath, pem.EncodeToMemory(block), 0o600); err != nil {
			devUserErr = err
			return
		}
		devUserDockerfile = fmt.Sprintf(`FROM alpine:3.24
RUN adduser -D -u 1234 dev && addgroup extra && addgroup dev extra \
 && adduser -D -u 1235 other \
 && mkdir -p /home/other/.ssh \
 && echo '%s' > /home/other/.ssh/authorized_keys \
 && chown -R other:other /home/other/.ssh && chmod 700 /home/other/.ssh
USER other
# Exits on SIGTERM, so stopping the container does not wait out Docker's 10 s.
CMD ["sh", "-c", "trap 'exit 0' TERM; tail -f /dev/null & wait"]
`, strings.TrimSpace(string(gossh.MarshalAuthorizedKey(sshPub))))
	})
	if devUserErr != nil {
		return nil, devUserErr
	}
	return map[string]string{
		".devcontainer/devcontainer.json": `{
			"name": "{{name}}",
			"build": {"dockerfile": "Dockerfile"},
			"workspaceFolder": "/workspace",
			"remoteUser": "dev",
			"updateRemoteUserUID": false,
			"overrideCommand": false
		}`,
		".devcontainer/Dockerfile": devUserDockerfile,
	}, nil
}

var (
	devUserOnce       sync.Once
	devUserErr        error
	devUserDockerfile string
	otherKeyPath      string // the private key in other's authorized_keys
)
