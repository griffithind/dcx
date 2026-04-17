//go:build e2e

package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSSHServerE2E tests the built-in SSH server functionality.
func TestSSHServerE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create a simple workspace
	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

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
	statusOut := helpers.RunDCXInDirSuccess(t, workspace, "status")
	var containerName string
	for _, line := range strings.Split(statusOut, "\n") {
		if strings.Contains(line, "Name:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				containerName = parts[1]
			}
		}
	}
	require.NotEmpty(t, containerName, "should find container name in status output")

	// Verify SSH config entry was added
	t.Run("ssh_config_entry_added", func(t *testing.T) {
		home, err := os.UserHomeDir()
		require.NoError(t, err)

		configPath := filepath.Join(home, ".ssh", "config")
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)

		configStr := string(content)
		assert.Contains(t, configStr, "# DCX managed - "+containerName)
		assert.Contains(t, configStr, "Host "+hostname)
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
		if os.Getenv("SSH_AUTH_SOCK") == "" {
			t.Skip("SSH_AUTH_SOCK not set, skipping agent forwarding test")
		}

		// Check if SSH_AUTH_SOCK is set inside the container via SSH -A
		stdout, _, err := runSSHWithAgent(t, hostname, "env")
		require.NoError(t, err)
		assert.Contains(t, stdout, "SSH_AUTH_SOCK=", "SSH agent should be forwarded")
	})

	// Test dcx down removes SSH config
	t.Run("down_removes_ssh_config", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Contains(t, stdout, "removed")

		// Verify SSH config entry was removed
		home, err := os.UserHomeDir()
		require.NoError(t, err)

		configPath := filepath.Join(home, ".ssh", "config")
		content, err := os.ReadFile(configPath)
		if err == nil {
			configStr := string(content)
			assert.NotContains(t, configStr, "# DCX managed - "+containerName,
				"SSH config entry should be removed after down")
		}
		// If file doesn't exist, that's also fine
	})
}

// TestSSHServerMultipleContainersE2E tests SSH with multiple containers.
func TestSSHServerMultipleContainersE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Create two separate workspaces with unique container names
	devcontainerJSON1 := helpers.SimpleImageConfigWithName("alpine:latest", helpers.UniqueTestName(t)+"_1")
	devcontainerJSON2 := helpers.SimpleImageConfigWithName("alpine:latest", helpers.UniqueTestName(t)+"_2")
	workspace1 := helpers.CreateTempWorkspace(t, devcontainerJSON1)
	workspace2 := helpers.CreateTempWorkspace(t, devcontainerJSON2)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace1, "down")
		helpers.RunDCXInDir(t, workspace2, "down")
	})

	// Start both containers (SSH is always configured)
	stdout1 := helpers.RunDCXInDirSuccess(t, workspace1, "up")
	stdout2 := helpers.RunDCXInDirSuccess(t, workspace2, "up")

	// Extract hostnames
	hostname1 := extractSSHHostname(t, stdout1)
	hostname2 := extractSSHHostname(t, stdout2)

	require.NotEqual(t, hostname1, hostname2, "Different workspaces should have different hostnames")

	// Verify we can reach both containers and they have different hostnames
	t.Run("correct_routing", func(t *testing.T) {
		// Get hostname from each container - they should be different container IDs
		containerHostname1, _, err := runSSH(t, hostname1, "hostname")
		require.NoError(t, err)

		containerHostname2, _, err := runSSH(t, hostname2, "hostname")
		require.NoError(t, err)

		// Container hostnames (IDs) should be different
		assert.NotEqual(t, strings.TrimSpace(containerHostname1), strings.TrimSpace(containerHostname2),
			"Each SSH hostname should route to a different container")
	})
}

// TestSSHPortStableAcrossDownUpE2E verifies the SSH host port does not
// change when a workspace is torn down and brought back up. Stability is
// load-bearing: IDE clients (VS Code Remote-SSH, JetBrains Gateway,
// Claude Desktop) key `known_hosts` entries and persistent connection
// caches by (host, port) — a rotating port shows up as "connection
// refused" or a host-key-changed warning even though dcx is healthy.
func TestSSHPortStableAcrossDownUpE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	stdout1 := helpers.RunDCXInDirSuccess(t, workspace, "up")
	port1 := extractSSHPort(t, stdout1)

	// Tear down completely — removes the container, freeing the port.
	helpers.RunDCXInDirSuccess(t, workspace, "down")

	// Up again. The port must be the same as before.
	stdout2 := helpers.RunDCXInDirSuccess(t, workspace, "up")
	port2 := extractSSHPort(t, stdout2)

	assert.Equal(t, port1, port2,
		"SSH port changed across down+up: first=%d second=%d — IDE known_hosts entries break when this is not stable",
		port1, port2)

	// A third cycle for good measure.
	helpers.RunDCXInDirSuccess(t, workspace, "down")
	stdout3 := helpers.RunDCXInDirSuccess(t, workspace, "up")
	port3 := extractSSHPort(t, stdout3)
	assert.Equal(t, port1, port3, "SSH port drifted after three cycles")
}

// TestSSHServerAlwaysEnabledE2E verifies that SSH is always configured when running dcx up.
func TestSSHServerAlwaysEnabledE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// SSH is always configured
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Contains(t, stdout, "SSH configured", "SSH should always be configured with dcx up")
}

// TestSSHFromDifferentDirectoryE2E tests that SSH works from any directory.
// With the TCP transport the generated ~/.ssh/config is self-contained
// (HostName/Port/IdentityFile), so cwd has no bearing on whether the
// connection succeeds or on the resolved remote working directory.
func TestSSHFromDifferentDirectoryE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "ssh-dir-test",
		"image": "alpine:latest",
		"workspaceFolder": "/test-workspace",
		"remoteUser": "root"
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	hostname := extractSSHHostname(t, stdout)

	// Get the actual container name from status
	statusOut := helpers.RunDCXInDirSuccess(t, workspace, "status")
	var containerName string
	for _, line := range strings.Split(statusOut, "\n") {
		if strings.Contains(line, "Name:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				containerName = parts[1]
			}
		}
	}
	require.NotEmpty(t, containerName, "should find container name in status output")

	// Verify workspace_path label is set on container
	t.Run("workspace_path_label_set", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.workspace.path"}}`,
			containerName)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, workspace, labelValue,
			"workspace_path label should be set to the workspace directory")
	})

	// Test `ssh <host>` from /tmp. With TCP transport, cwd is irrelevant —
	// the generated block has HostName/Port baked in.
	t.Run("ssh_from_different_directory", func(t *testing.T) {
		cmd := exec.Command("ssh",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			hostname, "pwd")
		cmd.Dir = "/tmp"

		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "SSH from /tmp failed: %s", output)
		assert.Contains(t, string(output), "/test-workspace",
			"SSH should use correct working directory from config even when run from different directory")
	})

	t.Run("ssh_user_from_different_directory", func(t *testing.T) {
		cmd := exec.Command("ssh",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			hostname, "whoami")
		cmd.Dir = "/tmp"

		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "SSH whoami from /tmp failed: %s", output)
		assert.Contains(t, string(output), "root",
			"SSH should use correct user from config even when run from different directory")
	})
}

// TestSSHServerCleanupE2E tests that the SSH server process is cleaned up after SSH session ends.
func TestSSHServerCleanupE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Start with SSH enabled
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	hostname := extractSSHHostname(t, stdout)

	// Get container name for direct docker exec checks
	statusOut, _, err := helpers.RunDCXInDir(t, workspace, "status")
	require.NoError(t, err)

	var containerName string
	for _, line := range strings.Split(statusOut, "\n") {
		if strings.Contains(line, "Name:") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				containerName = parts[1]
			}
		}
	}
	require.NotEmpty(t, containerName, "should find container name")

	// Run SSH command (this starts ssh-server process in container)
	t.Run("ssh_server_cleanup_after_session", func(t *testing.T) {
		// Execute a command via SSH
		_, _, err := runSSH(t, hostname, "echo", "test")
		require.NoError(t, err)

		// Give a moment for cleanup to complete
		// (SSH session closes, docker exec exits, process terminates)

		// Verify no dcx ssh-server processes remain in container
		// Use [s] character class trick to prevent pgrep from matching itself
		checkCmd := exec.Command("docker", "exec", containerName, "sh", "-c", "pgrep -f '[s]sh-server' || echo 'no-processes'")
		output, _ := checkCmd.CombinedOutput()
		outputStr := strings.TrimSpace(string(output))

		assert.Equal(t, "no-processes", outputStr,
			"No ssh-server processes should remain in container after SSH session, but found: %s", outputStr)
	})

	// Run multiple SSH sessions and verify cleanup
	t.Run("multiple_sessions_cleanup", func(t *testing.T) {
		// Run 3 SSH sessions sequentially
		for i := 0; i < 3; i++ {
			_, _, err := runSSH(t, hostname, "echo", "session", string(rune('1'+i)))
			require.NoError(t, err)
		}

		// Verify no lingering processes
		// Use [s] character class trick to prevent pgrep from matching itself
		checkCmd := exec.Command("docker", "exec", containerName, "sh", "-c", "pgrep -f '[s]sh-server' || echo 'no-processes'")
		output, _ := checkCmd.CombinedOutput()
		outputStr := strings.TrimSpace(string(output))

		assert.Equal(t, "no-processes", outputStr,
			"No ssh-server processes should remain after multiple SSH sessions, but found: %s", outputStr)
	})
}

// TestSSHCommandHandlingE2E tests that SSH properly handles complex shell commands.
// This verifies the RawCommand() fix - commands with quotes and nested shells must work.
func TestSSHCommandHandlingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Start with SSH enabled
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	hostname := extractSSHHostname(t, stdout)

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

	cmd := exec.Command("ssh", sshArgs...)

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

	cmd := exec.Command("ssh", sshArgs...)

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

	cmd := exec.Command("ssh", sshArgs...)

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

	cmd := exec.Command("sftp", sftpArgs...)
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
