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

// TestSecretsDirIsTmpfsE2E guards against the SSH host private key being
// written to the container's writable layer when no runtime secrets are
// configured.
func TestSecretsDirIsTmpfsE2E(t *testing.T) {
	t.Parallel()

	env := alpineEnv(t)
	workspace := env.Dir

	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--",
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
	env := devUserEnv(t)
	workspace := env.Dir
	keyPath := otherKeyPath
	upOut := env.UpOutput
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

		owner, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "stat", "-c", "%U", "/tmp/sftp-upload.txt")
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
