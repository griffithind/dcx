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

// TestSSHServerE2E tests the built-in SSH server functionality.
func TestSSHServerE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	// Create a simple workspace
	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Get the env key for this workspace (used in SSH hostname)
	var envKey string

	// Test dcx up --ssh adds SSH config
	t.Run("up_with_ssh_flag", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up", "--ssh")
		assert.Contains(t, stdout, "SSH configured")
		assert.Contains(t, stdout, ".dcx")

		// Extract the hostname from output
		for _, line := range strings.Split(stdout, "\n") {
			if strings.Contains(line, "SSH configured") {
				parts := strings.Fields(line)
				for _, p := range parts {
					if strings.HasSuffix(p, ".dcx") {
						envKey = strings.TrimSuffix(p, ".dcx")
						break
					}
				}
			}
		}
		require.NotEmpty(t, envKey, "should extract envKey from SSH configured output")
	})

	hostname := envKey + ".dcx"

	// Verify SSH config entry was added
	t.Run("ssh_config_entry_added", func(t *testing.T) {
		home, err := os.UserHomeDir()
		require.NoError(t, err)

		configPath := filepath.Join(home, ".ssh", "config")
		content, err := os.ReadFile(configPath)
		require.NoError(t, err)

		configStr := string(content)
		assert.Contains(t, configStr, "# DCX managed - "+envKey)
		assert.Contains(t, configStr, "Host "+hostname)
		assert.Contains(t, configStr, "ProxyCommand")
		assert.Contains(t, configStr, "ssh --stdio "+envKey)
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
			assert.NotContains(t, configStr, "# DCX managed - "+envKey,
				"SSH config entry should be removed after down")
		}
		// If file doesn't exist, that's also fine
	})
}

// TestSSHServerMultipleContainersE2E tests SSH with multiple containers.
func TestSSHServerMultipleContainersE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	// Create two separate workspaces
	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace1 := helpers.CreateTempWorkspace(t, devcontainerJSON)
	workspace2 := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace1, "down")
		helpers.RunDCXInDir(t, workspace2, "down")
	})

	// Start both with SSH
	stdout1 := helpers.RunDCXInDirSuccess(t, workspace1, "up", "--ssh")
	stdout2 := helpers.RunDCXInDirSuccess(t, workspace2, "up", "--ssh")

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

// TestSSHServerWithoutFlagE2E tests that SSH is not configured without --ssh flag.
func TestSSHServerWithoutFlagE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Start without --ssh flag
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.NotContains(t, stdout, "SSH configured", "SSH should not be configured without --ssh flag")
}

// TestSSHFromDifferentDirectoryE2E tests that SSH works from any directory.
// This simulates VS Code Remote SSH behavior where the ProxyCommand is executed
// from an arbitrary working directory.
func TestSSHFromDifferentDirectoryE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	// Create workspace with a specific remoteUser to verify config is loaded correctly
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

	// Start container with SSH
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up", "--ssh")
	hostname := extractSSHHostname(t, stdout)
	envKey := strings.TrimSuffix(hostname, ".dcx")

	// Verify workspace_path label is set on container
	t.Run("workspace_path_label_set", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "io.github.dcx.workspace_path"}}`,
			"dcx_"+envKey)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, workspace, labelValue,
			"workspace_path label should be set to the workspace directory")
	})

	// Test SSH from /tmp (different directory than workspace)
	t.Run("ssh_from_different_directory", func(t *testing.T) {
		dcxBinary := helpers.GetDCXBinary(t)

		// Run SSH with ProxyCommand explicitly from /tmp
		sshArgs := []string{
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "LogLevel=ERROR",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			"-o", fmt.Sprintf("ProxyCommand=%s ssh --stdio %s", dcxBinary, envKey),
			hostname,
			"pwd",
		}

		cmd := exec.Command("ssh", sshArgs...)
		cmd.Dir = "/tmp" // Run from different directory

		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "SSH from /tmp failed: %s", output)

		// Verify working directory is correct (from devcontainer.json)
		assert.Contains(t, string(output), "/test-workspace",
			"SSH should use correct working directory from config even when run from different directory")
	})

	// Test that correct user is used when running from different directory
	t.Run("ssh_user_from_different_directory", func(t *testing.T) {
		dcxBinary := helpers.GetDCXBinary(t)

		sshArgs := []string{
			"-o", "StrictHostKeyChecking=no",
			"-o", "UserKnownHostsFile=/dev/null",
			"-o", "LogLevel=ERROR",
			"-o", "BatchMode=yes",
			"-o", "ConnectTimeout=10",
			"-o", fmt.Sprintf("ProxyCommand=%s ssh --stdio %s", dcxBinary, envKey),
			hostname,
			"whoami",
		}

		cmd := exec.Command("ssh", sshArgs...)
		cmd.Dir = "/tmp"

		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "SSH whoami from /tmp failed: %s", output)

		// Verify user is correct (from devcontainer.json remoteUser)
		assert.Contains(t, string(output), "root",
			"SSH should use correct user from config even when run from different directory")
	})
}

// TestSSHServerCleanupE2E tests that the SSH server process is cleaned up after SSH session ends.
func TestSSHServerCleanupE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Start with SSH enabled
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up", "--ssh")
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

// Helper functions

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
