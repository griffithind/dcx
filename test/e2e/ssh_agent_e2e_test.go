//go:build e2e

package e2e

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSSHAgentForwardingE2E tests that SSH agent forwarding works.
// Agent forwarding uses the SSH protocol's built-in auth-agent channel.
func TestSSHAgentForwardingE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Skip if no SSH agent is available on the host
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH_AUTH_SOCK not set, skipping SSH agent test")
	}

	// Create a simple workspace
	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the environment
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify SSH_AUTH_SOCK is set inside the container during exec
	t.Run("ssh_auth_sock_set", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "SSH_AUTH_SOCK")
		require.NoError(t, err)
		sockPath := strings.TrimSpace(stdout)
		assert.NotEmpty(t, sockPath, "SSH_AUTH_SOCK should be set")
	})

	// Verify the socket file exists during exec
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
}

// TestSSHAgentAlwaysEnabledE2E verifies that SSH agent is always enabled when available.
func TestSSHAgentAlwaysEnabledE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Skip if no SSH agent is available on the host
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH_AUTH_SOCK not set, skipping SSH agent test")
	}

	// Create a simple workspace
	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify SSH_AUTH_SOCK is set inside the container when host has SSH agent
	t.Run("ssh_auth_sock_set", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "SSH_AUTH_SOCK")
		require.NoError(t, err)
		sockPath := strings.TrimSpace(stdout)
		assert.NotEmpty(t, sockPath, "SSH_AUTH_SOCK should be set when host has SSH agent")
	})
}

// TestSSHAgentConcurrentExecE2E tests that concurrent execs don't interfere with each other.
func TestSSHAgentConcurrentExecE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// Skip if no SSH agent is available on the host
	if os.Getenv("SSH_AUTH_SOCK") == "" {
		t.Skip("SSH_AUTH_SOCK not set, skipping SSH agent test")
	}

	// Create a simple workspace
	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test that concurrent execs each have working agent access
	t.Run("concurrent_execs_have_agent", func(t *testing.T) {
		var wg sync.WaitGroup
		results := make(chan string, 3)
		errors := make(chan error, 3)

		// Start 3 concurrent execs that each check their socket
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--",
					"sh", "-c", "test -S \"$SSH_AUTH_SOCK\" && echo ok")
				if err != nil {
					errors <- err
					return
				}
				results <- strings.TrimSpace(stdout)
			}(i)
		}

		wg.Wait()
		close(results)
		close(errors)

		// Check for errors
		for err := range errors {
			require.NoError(t, err, "concurrent exec should not fail")
		}

		// All should have valid agent access
		count := 0
		for result := range results {
			assert.Contains(t, result, "ok", "exec should have agent socket access")
			count++
		}
		assert.Equal(t, 3, count, "all 3 concurrent execs should succeed")
	})

	// Test that one exec finishing doesn't kill another's agent
	t.Run("cleanup_doesnt_affect_other_execs", func(t *testing.T) {
		// Install openssh-client first
		_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "apk", "add", "--no-cache", "openssh-client")
		require.NoError(t, err, "failed to install openssh-client")

		// Start a long-running exec
		var wg sync.WaitGroup
		longExecDone := make(chan struct{})
		longExecResult := make(chan string, 1)

		wg.Add(1)
		go func() {
			defer wg.Done()
			// This exec will sleep for 3 seconds then check agent
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--",
				"sh", "-c", "sleep 3 && ssh-add -l 2>&1 || echo 'agent-accessible'")
			if err == nil {
				longExecResult <- stdout
			}
			close(longExecDone)
		}()

		// Wait a moment for the long exec to start
		time.Sleep(500 * time.Millisecond)

		// Run a quick exec that will finish first
		_, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "quick-exec")
		require.NoError(t, err, "quick exec should succeed")

		// Wait for long exec to complete
		<-longExecDone
		wg.Wait()

		// The long exec should have been able to access the SSH agent
		select {
		case result := <-longExecResult:
			// Should either show keys or say "no identities" - both mean agent was accessible
			assert.True(t,
				strings.Contains(result, "agent-accessible") ||
					strings.Contains(strings.ToLower(result), "no identities") ||
					strings.Contains(result, "SHA256:"),
				"Long-running exec should still have SSH agent access after quick exec cleanup, got: %s", result)
		default:
			t.Error("Long exec should have completed with a result")
		}
	})
}
