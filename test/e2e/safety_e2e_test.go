//go:build e2e

package e2e

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
)

// TestStopStartKeepsEnvironmentUsableE2E guards against `dcx up` on a stopped
// container (and `dcx restart`) only running `docker start`: the in-container
// SSH agent dies with the container, so exec must keep working afterwards and
// postStartCommand must run on every start.
func TestStopStartKeepsEnvironmentUsableE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"postStartCommand": "echo started >> /tmp/poststart.log"
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down") })

	execEcho := func(t *testing.T, marker string) {
		t.Helper()
		stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", marker)
		require.NoError(t, err, "exec failed: stdout=%s stderr=%s", stdout, stderr)
		assert.Contains(t, stdout, marker)
	}
	postStartRuns := func(t *testing.T) int {
		t.Helper()
		stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/poststart.log")
		require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
		return strings.Count(stdout, "started")
	}

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	execEcho(t, "exec-after-create")
	assert.Equal(t, 1, postStartRuns(t))

	t.Run("stop_then_up", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "stop")
		helpers.RunDCXInDirSuccess(t, workspace, "up")
		execEcho(t, "exec-after-stop-up")
		assert.Equal(t, 2, postStartRuns(t), "postStartCommand should run on every start")
	})

	t.Run("restart", func(t *testing.T) {
		helpers.RunDCXInDirSuccess(t, workspace, "restart")
		execEcho(t, "exec-after-restart")
		assert.Equal(t, 3, postStartRuns(t), "postStartCommand should run after restart")
	})
}

// TestRecreatePreservesComposeNamedVolumesE2E guards against a config change
// (which marks the environment stale and recreates it) running
// `docker compose down -v` and deleting the user's named volumes.
func TestRecreatePreservesComposeNamedVolumesE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	name := helpers.UniqueTestName(t)
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`, name)
	dockerComposeYAML := `services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
      - data:/data
volumes:
  data:
`
	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down", "--volumes") })

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	_, stderr, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo keep-me > /data/marker")
	require.NoError(t, err, stderr)

	// Change the config so the environment becomes stale and is recreated.
	changed := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"containerEnv": {"CONFIG_CHANGED": "1"}
	}`, name)
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".devcontainer", "devcontainer.json"), []byte(changed), 0644))
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/data/marker")
	require.NoError(t, err, "named volume content lost after recreate: stdout=%s stderr=%s", stdout, stderr)
	assert.Contains(t, stdout, "keep-me")
}

// TestSecretsDirIsTmpfsE2E guards against the SSH host private key being
// written to the container's writable layer when no runtime secrets are
// configured.
func TestSecretsDirIsTmpfsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := helpers.CreateTempWorkspace(t, helpers.SimpleImageConfig(t, "alpine:latest"))
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down") })

	helpers.RunDCXInDirSuccess(t, workspace, "up")
	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "exec", "--",
		"awk", `$2=="/run/secrets"{print $3}`, "/proc/mounts")
	require.NoError(t, err, stderr)
	assert.Equal(t, "tmpfs", strings.TrimSpace(stdout), "/run/secrets must be a tmpfs mount")
}

// TestAgentPrivilegeBoundariesE2E guards the in-container SSH agent's
// privilege model when it runs as root and sessions drop to remoteUser:
//   - a key in another user's ~/.ssh/authorized_keys must not grant a session
//     (sessions always run as remoteUser, so that would be an escalation);
//   - SFTP must run as remoteUser, not as the agent's root identity.
func TestAgentPrivilegeBoundariesE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Key that an unprivileged container user ("other") has added to its own
	// authorized_keys.
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	sshPub, err := gossh.NewPublicKey(pub)
	require.NoError(t, err)
	authLine := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(sshPub)))

	keyDir := t.TempDir()
	keyPath := filepath.Join(keyDir, "other_key")
	block, err := gossh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600))

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"remoteUser": "dev",
		"updateRemoteUserUID": false
	}`, helpers.UniqueTestName(t))
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)
	dockerfile := fmt.Sprintf(`FROM alpine:latest
RUN adduser -D -u 1234 dev && adduser -D -u 1235 other \
 && mkdir -p /home/other/.ssh \
 && echo '%s' > /home/other/.ssh/authorized_keys \
 && chown -R other:other /home/other/.ssh && chmod 700 /home/other/.ssh
`, authLine)
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".devcontainer", "Dockerfile"), []byte(dockerfile), 0644))
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down") })

	upOut := helpers.RunDCXInDirSuccess(t, workspace, "up")
	hostname := extractSSHHostname(t, upOut)

	t.Run("foreign_authorized_keys_rejected", func(t *testing.T) {
		// Resolve the forwarded port from the generated ssh config, then
		// connect with an isolated config (-F /dev/null) so only the foreign
		// key is offered; the dcx-managed config would otherwise also offer
		// the legitimately authorized ~/.dcx/id_ed25519.
		resolvedCfg, stderr, err := runCommand(t, "ssh", "-G", hostname)
		require.NoError(t, err, stderr)
		var host, port string
		for _, line := range strings.Split(resolvedCfg, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 && fields[0] == "hostname" {
				host = fields[1]
			}
			if len(fields) == 2 && fields[0] == "port" {
				port = fields[1]
			}
		}
		require.NotEmpty(t, host)
		require.NotEmpty(t, port)

		cmd := []string{
			"-F", "/dev/null",
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "LogLevel=ERROR",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			"-o", "IdentitiesOnly=yes",
			"-o", "IdentityAgent=none",
			"-i", keyPath,
			"-p", port,
			"-l", "other",
			host, "whoami",
		}
		stdout, _, err := runCommand(t, "ssh", cmd...)
		assert.Error(t, err, "a key from another user's authorized_keys must not authenticate (got session as %q)", strings.TrimSpace(stdout))
	})

	t.Run("sftp_runs_as_remote_user", func(t *testing.T) {
		local := filepath.Join(t.TempDir(), "upload.txt")
		require.NoError(t, os.WriteFile(local, []byte("hi"), 0644))

		stdout, stderr, err := runSFTP(t, hostname, fmt.Sprintf("put %s /tmp/sftp-upload.txt", local))
		require.NoError(t, err, "sftp put failed: stdout=%s stderr=%s", stdout, stderr)

		owner, stderr, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "stat", "-c", "%U", "/tmp/sftp-upload.txt")
		require.NoError(t, err, stderr)
		assert.Equal(t, "dev", strings.TrimSpace(owner), "SFTP-created files must be owned by remoteUser")
	})
}

// runCommand runs an external command and returns its stdout, stderr and error.
func runCommand(t *testing.T, name string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}
