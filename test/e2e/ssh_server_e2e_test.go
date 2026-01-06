//go:build e2e

package e2e

import (
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
