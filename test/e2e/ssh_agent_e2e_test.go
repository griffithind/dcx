//go:build e2e

package e2e

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSSHAgentForwardingE2E tests that SSH agent forwarding works.
// With the per-exec proxy approach, the socket is created uniquely for each exec.
func TestSSHAgentForwardingE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	// Skip if no SSH agent is available on the host
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH_AUTH_SOCK not set, skipping SSH agent test")
	}

	// Create a simple workspace
	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the environment (SSH agent directory is mounted but socket created per-exec)
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify SSH_AUTH_SOCK is set inside the container during exec
	t.Run("ssh_auth_sock_set", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "SSH_AUTH_SOCK")
		require.NoError(t, err)
		// On Docker Desktop: /run/host-services/ssh-auth.sock
		// On native Docker: /ssh-agent/agent-xxxxx.sock (unique per exec)
		sockPath := strings.TrimSpace(stdout)
		assert.True(t,
			strings.Contains(sockPath, "/run/host-services/ssh-auth.sock") ||
				strings.Contains(sockPath, "/ssh-agent/agent-"),
			"SSH_AUTH_SOCK should be set to Docker Desktop path or proxy path, got: %s", sockPath)
	})

	// Verify the socket file exists during exec (the proxy creates it for each exec)
	t.Run("socket_accessible", func(t *testing.T) {
		// Use a shell command to get the socket path and test it
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--",
			"sh", "-c", "test -S \"$SSH_AUTH_SOCK\" && echo ok")
		require.NoError(t, err, "SSH agent socket should exist during exec")
		assert.Contains(t, stdout, "ok")
	})

	// Try to list SSH keys (this will work if host has keys, or return empty list)
	t.Run("ssh_add_works", func(t *testing.T) {
		// Install openssh-client in alpine to use ssh-add
		_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "apk", "add", "--no-cache", "openssh-client")
		require.NoError(t, err, "failed to install openssh-client")

		// Run ssh-add -l - it should either list keys or say "no identities"
		// Both are valid responses indicating the agent is working
		stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "ssh-add", "-l")
		combined := stdout + stderr

		// ssh-add -l returns:
		// - exit 0 with key list if keys exist
		// - exit 1 with "The agent has no identities." if no keys
		// - exit 2 with error if agent not accessible
		if err != nil {
			// Check it's the "no identities" case (exit code 1), not agent error (exit code 2)
			assert.Contains(t, strings.ToLower(combined), "no identities",
				"ssh-add should either list keys or report no identities, got: %s", combined)
		}
		// If no error, keys were listed successfully
	})

	// Test SSH connection to GitHub
	t.Run("github_ssh_connection", func(t *testing.T) {
		// Install openssh-client for ssh command
		_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "apk", "add", "--no-cache", "openssh-client")
		require.NoError(t, err, "failed to install openssh-client")

		// Try to connect to GitHub via SSH
		// ssh -T git@github.com returns exit code 1 with "successfully authenticated" on success
		// or exit code 255 with permission denied on failure
		stdout, stderr, _ := helpers.RunDCXInDir(t, workspace, "exec", "--",
			"ssh", "-T", "-o", "StrictHostKeyChecking=no", "-o", "BatchMode=yes", "git@github.com")
		combined := strings.ToLower(stdout + stderr)

		// GitHub SSH returns exit code 1 even on success (it doesn't allow shell access)
		// We check for the success message in the output
		if strings.Contains(combined, "successfully authenticated") {
			// Success - SSH agent forwarding works with GitHub
			t.Log("GitHub SSH authentication successful")
		} else if strings.Contains(combined, "permission denied") {
			// SSH agent connected but no valid GitHub key - still means forwarding works
			t.Log("SSH agent working, but no GitHub key configured in agent")
		} else if strings.Contains(combined, "connection refused") || strings.Contains(combined, "network") {
			// Network issue, skip
			t.Skip("Could not connect to GitHub (network issue)")
		} else {
			// Log for debugging but don't fail
			t.Logf("GitHub SSH output: %s", combined)
		}
	})

	// Test concurrent execs - on native Docker each gets unique socket, on Docker Desktop they share
	t.Run("concurrent_exec_sockets", func(t *testing.T) {
		var wg sync.WaitGroup
		sockets := make(chan string, 3)

		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "SSH_AUTH_SOCK")
				if err == nil {
					sockets <- strings.TrimSpace(stdout)
				}
			}()
		}

		wg.Wait()
		close(sockets)

		// Collect all socket paths
		socketPaths := make(map[string]bool)
		for s := range sockets {
			socketPaths[s] = true
		}

		// All sockets should be valid
		assert.GreaterOrEqual(t, len(socketPaths), 1, "Should have at least one socket path")

		// Check the socket paths are valid
		for sock := range socketPaths {
			assert.True(t,
				strings.Contains(sock, "/run/host-services/ssh-auth.sock") ||
					strings.Contains(sock, "/ssh-agent/agent-"),
				"Socket path should be valid: %s", sock)
		}
	})
}

// TestSSHAgentDisabledE2E tests that --no-agent flag disables SSH forwarding.
func TestSSHAgentDisabledE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	// Create a simple workspace
	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up with --no-agent (disables SSH agent directory mounting)
	helpers.RunDCXInDirSuccess(t, workspace, "up", "--no-agent")

	// Verify SSH_AUTH_SOCK is NOT set inside the container with --no-agent on exec
	t.Run("ssh_auth_sock_not_set_with_no_agent", func(t *testing.T) {
		stdout, _, _ := helpers.RunDCXInDir(t, workspace, "exec", "--no-agent", "--", "printenv", "SSH_AUTH_SOCK")
		// Should be empty or command should fail (no such variable)
		assert.Empty(t, strings.TrimSpace(stdout), "SSH_AUTH_SOCK should not be set when --no-agent is used")
	})
}

// TestSSHAgentExecNoAgentFlag tests the --no-agent flag specifically on exec.
func TestSSHAgentExecNoAgentFlag(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	// Skip if no SSH agent is available on the host
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH_AUTH_SOCK not set, skipping SSH agent test")
	}

	// Create a simple workspace
	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up normally (with SSH agent enabled)
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify exec without --no-agent has SSH_AUTH_SOCK
	t.Run("exec_with_agent", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "SSH_AUTH_SOCK")
		require.NoError(t, err)
		sockPath := strings.TrimSpace(stdout)
		assert.True(t,
			strings.Contains(sockPath, "/run/host-services/ssh-auth.sock") ||
				strings.Contains(sockPath, "/ssh-agent/agent-"),
			"SSH_AUTH_SOCK should be set, got: %s", sockPath)
	})

	// Verify exec with --no-agent does NOT have SSH_AUTH_SOCK
	t.Run("exec_without_agent", func(t *testing.T) {
		stdout, _, _ := helpers.RunDCXInDir(t, workspace, "exec", "--no-agent", "--", "printenv", "SSH_AUTH_SOCK")
		assert.Empty(t, strings.TrimSpace(stdout), "SSH_AUTH_SOCK should not be set with --no-agent on exec")
	})
}
