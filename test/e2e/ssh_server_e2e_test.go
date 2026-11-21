//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSSHServerE2E walks the in-container SSH server through a workspace's
// life: up writes the SSH config, OpenSSH sessions and SFTP work, session
// processes exit with their sessions, a plain OpenSSH session gets what
// `dcx ssh` gives (remoteEnv with ${containerEnv:…} and, for interactive
// sessions only, postAttachCommand), and down removes the SSH config. It
// counts the container's processes, so it has a container of its own.
func TestSSHServerE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"workspaceMount": "type=bind,source=${localWorkspaceFolder},target=/workspace",
		"containerEnv": {"BASE": "base-value"},
		"remoteEnv": {"FROM_REMOTE": "remote-${containerEnv:BASE}"},
		"postAttachCommand": "echo attached >> /tmp/attach.log"
	}`, helpers.UniqueTestName(t)))

	// Get the workspace ID for this workspace (used in SSH hostname)
	var workspaceID string

	// Test dcx up adds SSH config (SSH is always enabled)
	t.Run("up_configures_ssh", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "SSH configured")
		assert.Contains(t, stdout, ".dcx")

		// Extract the hostname from output
		for _, line := range strings.Split(stdout, "\n") {
			if strings.Contains(line, "SSH configured") {
				parts := strings.Fields(line)
				for _, p := range parts {
					if strings.HasSuffix(p, ".dcx") {
						workspaceID = strings.TrimSuffix(p, ".dcx")
						break
					}
				}
			}
		}
		require.NotEmpty(t, workspaceID, "should extract workspaceID from SSH configured output")
	})

	hostname := workspaceID + ".dcx"

	// Get the actual container name from status
	containerName := helpers.ContainerName(t, workspace)

	// Verify SSH config entry was added
	t.Run("ssh_config_entry_added", func(t *testing.T) {
		// Blocks live in <ssh dir>/dcx/config, included from <ssh dir>/config
		// (the run's isolated stand-in for ~/.ssh).
		sshDir := helpers.SSHDir()
		require.NotEmpty(t, sshDir)
		mainConfig, err := os.ReadFile(filepath.Join(sshDir, "config"))
		require.NoError(t, err)
		assert.Contains(t, string(mainConfig), "Include "+filepath.Join(sshDir, "dcx", "config"))
		assert.NotContains(t, string(mainConfig), "# DCX managed - "+containerName)

		configPath := filepath.Join(sshDir, "dcx", "config")
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)

		configStr := string(content)
		assert.Contains(t, configStr, "# DCX managed - "+containerName)
		assert.Contains(t, configStr, "Host "+hostname+" "+containerName+".dcx")
		// TCP transport: config points at 127.0.0.1:<ephemeral port>, never
		// a ProxyCommand stdio tunnel.
		assert.Contains(t, configStr, "HostName 127.0.0.1")
		assert.Regexp(t, `Port \d+`, configStr)
		assert.NotContains(t, configStr, "ProxyCommand")
		assert.NotContains(t, configStr, "--stdio")
	})

	// Test SSH connection works
	t.Run("ssh_connection", func(t *testing.T) {
		stdout, stderr, err := runSSH(t, hostname, "echo", "hello-from-ssh")
		require.NoError(t, err, "SSH connection failed: stdout=%s stderr=%s", stdout, stderr)
		assert.Contains(t, stdout, "hello-from-ssh")
	})

	// Test SSH command execution
	t.Run("ssh_exec_whoami", func(t *testing.T) {
		stdout, _, err := runSSH(t, hostname, "whoami")
		require.NoError(t, err)
		assert.Contains(t, stdout, "root")
	})

	// Test working directory
	t.Run("ssh_workdir", func(t *testing.T) {
		stdout, _, err := runSSH(t, hostname, "pwd")
		require.NoError(t, err)
		assert.Contains(t, stdout, "/workspace")
	})

	// Test SFTP works
	t.Run("sftp_connection", func(t *testing.T) {
		// Use sftp to list the workspace directory
		stdout, stderr, err := runSFTP(t, hostname, "ls")
		require.NoError(t, err, "SFTP failed: stdout=%s stderr=%s", stdout, stderr)
		// Should show the sftp prompt and execute ls without error
		assert.Contains(t, stdout, "sftp>")
	})

	// Test SSH agent forwarding via SSH connection
	t.Run("ssh_agent_forwarding", func(t *testing.T) {
		// Check if SSH_AUTH_SOCK is set inside the container via SSH -A
		stdout, _, err := runSSHWithAgent(t, hostname, "env")
		require.NoError(t, err)
		assert.Contains(t, stdout, "SSH_AUTH_SOCK=", "SSH agent should be forwarded")
	})

	// agentChildren counts processes whose parent is the dcx-agent
	// listener: every session runs as one, so after sessions end it is 0.
	agentChildren := func(t *testing.T) string {
		t.Helper()
		script := `a=$(pgrep -f '[d]cx-agent listen' | head -n1)
[ -n "$a" ] || { echo no-agent; exit 0; }
n=0
for s in /proc/[0-9]*/stat; do
  set -- $(cat "$s" 2>/dev/null)
  [ "$4" = "$a" ] && n=$((n+1))
done
echo "$n"`
		out, err := exec.Command("docker", "exec", containerName, "sh", "-c", script).CombinedOutput()
		require.NoError(t, err, string(out))
		return strings.TrimSpace(string(out))
	}

	t.Run("sessions_cleaned_up", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			_, _, err := runSSH(t, hostname, "echo", "session", string(rune('1'+i)))
			require.NoError(t, err)
		}
		assert.Eventually(t, func() bool { return agentChildren(t) == "0" }, 5*time.Second, 100*time.Millisecond,
			"session processes must exit with their sessions")
	})

	t.Run("plain_ssh_session_parity", func(t *testing.T) {
		out, err := plainSSH(t, hostname, "", "printenv", "FROM_REMOTE")
		require.NoError(t, err, out)
		assert.Equal(t, "remote-base-value", strings.TrimSpace(out))

		_, _, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "test", "-f", "/tmp/attach.log")
		assert.Error(t, err, "postAttachCommand must not run for exec sessions")

		out, err = plainSSH(t, hostname, "exit\n", "-tt")
		require.NoError(t, err, out)
		assert.Equal(t, "attached", dcxExec(t, workspace, "cat", "/tmp/attach.log"))

		// An interactive `dcx ssh` gets it from the agent as well, once per attach.
		shellOut, err := helpers.RunDCXInPTY(t, workspace, "exit\n", "ssh")
		require.NoError(t, err, shellOut)
		assert.Equal(t, "attached\nattached", dcxExec(t, workspace, "cat", "/tmp/attach.log"))
	})

	// Test dcx down removes SSH config
	t.Run("down_removes_ssh_config", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Contains(t, stdout, "removed")

		// Verify SSH config entry was removed

		for _, f := range []string{"config", filepath.Join("dcx", "config")} {
			content, err := os.ReadFile(filepath.Join(helpers.SSHDir(), f))
			if err == nil {
				assert.NotContains(t, string(content), "# DCX managed - "+containerName,
					"SSH config entry should be removed after down (%s)", f)
			}
		}
	})
}

// TestSSHServerMultipleContainersE2E: two workspaces get different aliases,
// each reaching its own container.
func TestSSHServerMultipleContainersE2E(t *testing.T) {
	t.Parallel()
	hostname1 := extractSSHHostname(t, alpineEnv(t).UpOutput)
	hostname2 := extractSSHHostname(t, richEnv(t).UpOutput)
	require.NotEqual(t, hostname1, hostname2, "Different workspaces should have different hostnames")

	containerHostname1, _, err := runSSH(t, hostname1, "hostname")
	require.NoError(t, err)
	containerHostname2, _, err := runSSH(t, hostname2, "hostname")
	require.NoError(t, err)
	assert.NotEqual(t, strings.TrimSpace(containerHostname1), strings.TrimSpace(containerHostname2),
		"Each SSH hostname should route to a different container")
}

// TestSSHServerAlwaysEnabledE2E verifies that SSH is always configured when running dcx up.
func TestSSHServerAlwaysEnabledE2E(t *testing.T) {
	t.Parallel()

	env := alpineEnv(t)

	// SSH is always configured
	stdout := env.UpOutput
	assert.Contains(t, stdout, "SSH configured", "SSH should always be configured with dcx up")
}

// TestSSHFromDifferentDirectoryE2E: `ssh <alias>` run from any directory
// gets the workspace's working directory and user (they come from the
// agent, not from where ssh runs), and the container records the workspace
// path.
func TestSSHFromDifferentDirectoryE2E(t *testing.T) {
	t.Parallel()
	env := alpineEnv(t)
	hostname := extractSSHHostname(t, env.UpOutput)

	output, err := exec.Command("docker", "inspect", "--format",
		`{{index .Config.Labels "com.griffithind.dcx.workspace.path"}}`, helpers.ContainerName(t, env.Dir)).CombinedOutput()
	require.NoError(t, err, "failed to inspect container: %s", output)
	assert.Equal(t, env.Dir, strings.TrimSpace(string(output)), "workspace_path label should be set to the workspace directory")

	for cmd, want := range map[string]string{"pwd": "/workspace", "whoami": "root"} {
		c := helpers.SSHCommand("-o", "BatchMode=yes", "-o", "ConnectTimeout=10", hostname, cmd)
		c.Dir = "/tmp"
		out, err := c.CombinedOutput()
		require.NoError(t, err, "ssh %s from /tmp: %s", cmd, out)
		assert.Contains(t, string(out), want, "ssh %s from another directory", cmd)
	}
}

// TestSSHCommandHandlingE2E tests that SSH properly handles complex shell commands.
// This verifies the RawCommand() fix - commands with quotes and nested shells must work.
func TestSSHCommandHandlingE2E(t *testing.T) {
	t.Parallel()

	hostname := extractSSHHostname(t, alpineEnv(t).UpOutput)

	// Test simple echo
	t.Run("simple_echo", func(t *testing.T) {
		stdout, _, err := runSSH(t, hostname, "echo", "hello")
		require.NoError(t, err)
		assert.Contains(t, stdout, "hello")
	})

	// Test nested sh -c with quotes (the main fix)
	t.Run("nested_sh_c_with_quotes", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `sh -c "echo nested_test"`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "nested_test")
	})

	// Test cd followed by command (Zed editor pattern)
	t.Run("cd_and_command", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `cd; uname -sm`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "Linux")
	})

	// Test command with && operator
	t.Run("and_operator", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `echo first && echo second`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "first")
		assert.Contains(t, stdout, "second")
	})

	// Test command with || operator
	t.Run("or_operator", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `false || echo fallback`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "fallback")
	})

	// Test command with pipe
	t.Run("pipe_operator", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `echo "hello world" | tr ' ' '_'`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "hello_world")
	})

	// Test semicolon-separated commands
	t.Run("semicolon_commands", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `echo one; echo two; echo three`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "one")
		assert.Contains(t, stdout, "two")
		assert.Contains(t, stdout, "three")
	})

	// Test command with single quotes inside double quotes
	t.Run("nested_quotes", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `echo "it's working"`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "it's working")
	})

	// Test deeply nested sh -c (VS Code/Zed pattern)
	t.Run("deeply_nested_sh", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `cd; sh -c 'echo "deep_nested"'`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "deep_nested")
	})

	// Test variable expansion
	t.Run("variable_expansion", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `VAR=testval; echo $VAR`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "testval")
	})

	// Test glob expansion
	t.Run("glob_expansion", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `ls /etc/*.conf 2>/dev/null | head -1`)
		require.NoError(t, err, "stderr: %s", stderr)
		// Should return at least one .conf file
		assert.Contains(t, stdout, ".conf")
	})

	// Test redirection
	t.Run("redirection", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `echo redirect_test > /tmp/ssh_test.txt && cat /tmp/ssh_test.txt`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "redirect_test")
	})

	// Test subshell
	t.Run("subshell", func(t *testing.T) {
		stdout, stderr, err := runSSHRaw(t, hostname, `echo $(echo subshell_output)`)
		require.NoError(t, err, "stderr: %s", stderr)
		assert.Contains(t, stdout, "subshell_output")
	})
}

// Helper functions

// runSSHRaw runs an SSH command passing the command as a single string (like VS Code/Zed do).
func runSSHRaw(t *testing.T, hostname string, command string) (string, string, error) {
	t.Helper()

	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		hostname,
		command,
	}

	cmd := helpers.SSHCommand(sshArgs...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func runSSH(t *testing.T, hostname string, args ...string) (string, string, error) {
	t.Helper()

	sshArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		hostname,
	}
	sshArgs = append(sshArgs, args...)

	cmd := helpers.SSHCommand(sshArgs...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func runSSHWithAgent(t *testing.T, hostname string, args ...string) (string, string, error) {
	t.Helper()

	sshArgs := []string{
		"-A", // Enable agent forwarding
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		hostname,
	}
	sshArgs = append(sshArgs, args...)

	cmd := helpers.SSHCommand(sshArgs...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func runSFTP(t *testing.T, hostname string, command string) (string, string, error) {
	t.Helper()

	sftpArgs := []string{
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "BatchMode=yes",
		"-b", "-", // Read commands from stdin
		hostname,
	}

	cmd := helpers.SFTPCommand(sftpArgs...)
	cmd.Stdin = strings.NewReader(command + "\n")

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func extractSSHHostname(t *testing.T, output string) string {
	t.Helper()

	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "SSH configured") {
			parts := strings.Fields(line)
			for _, p := range parts {
				if strings.HasSuffix(p, ".dcx") {
					return p
				}
			}
		}
	}

	t.Fatal("Could not extract SSH hostname from output")
	return ""
}

// extractSSHPort scrapes the "(127.0.0.1:NNNNN)" fragment dcx prints in
// its `SSH configured: …` success line.
func extractSSHPort(t *testing.T, output string) int {
	t.Helper()

	re := regexp.MustCompile(`127\.0\.0\.1:(\d+)`)
	for _, line := range strings.Split(output, "\n") {
		if m := re.FindStringSubmatch(line); len(m) == 2 {
			n, err := strconv.Atoi(m[1])
			if err == nil {
				return n
			}
		}
	}
	t.Fatalf("Could not extract SSH port from output:\n%s", output)
	return 0
}
