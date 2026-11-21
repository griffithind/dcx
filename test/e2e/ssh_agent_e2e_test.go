//go:build e2e

package e2e

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSSHAgentForwardingE2E tests that SSH agent forwarding works (the
// SSH protocol's auth-agent channel) with the agent helpers.Isolate serves.
func TestSSHAgentForwardingE2E(t *testing.T) {
	t.Parallel()

	// Create a simple workspace
	env := alpineEnv(t)
	workspace := env.Dir

	// Verify SSH_AUTH_SOCK is set inside the container during exec
	t.Run("ssh_auth_sock_set", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "printenv", "SSH_AUTH_SOCK")
		require.NoError(t, err)
		sockPath := strings.TrimSpace(stdout)
		assert.NotEmpty(t, sockPath, "SSH_AUTH_SOCK should be set")
	})

	// Verify the socket file exists during exec
	t.Run("socket_accessible", func(t *testing.T) {
		// Use a shell command to get the socket path and test it
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--",
			"sh", "-c", "test -S \"$SSH_AUTH_SOCK\" && echo ok")
		require.NoError(t, err, "SSH agent socket should exist during exec")
		assert.Contains(t, stdout, "ok")
	})

	// The forwarded agent is the e2e run's agent (helpers.Isolate): it
	// lists exactly that agent's key.
	t.Run("ssh_add_lists_the_host_key", func(t *testing.T) {
		requireSSHClient(t, workspace)

		stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "ssh-add", "-l")
		require.NoError(t, err, "ssh-add -l: %s", stderr)
		assert.Contains(t, stdout, helpers.AgentKeyFingerprint())
	})
}

// TestSSHAgentConcurrentExecE2E tests that concurrent execs don't interfere with each other.
func TestSSHAgentConcurrentExecE2E(t *testing.T) {
	t.Parallel()

	// Create a simple workspace
	env := alpineEnv(t)
	workspace := env.Dir

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
				stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--",
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
		requireSSHClient(t, workspace)

		// Start a long-running exec
		var wg sync.WaitGroup
		longExecDone := make(chan struct{})
		longExecResult := make(chan string, 1)

		wg.Add(1)
		go func() {
			defer wg.Done()
			// This exec will sleep for 3 seconds then check agent
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--",
				// ssh-add exits 0 when it reaches the agent (which holds a
				// key), and 2 when it can't connect.
				"sh", "-c", "sleep 3; ssh-add -l >/dev/null 2>&1; echo \"exit=$?\"")
			if err == nil {
				longExecResult <- stdout
			}
			close(longExecDone)
		}()

		// Wait a moment for the long exec to start
		time.Sleep(500 * time.Millisecond)

		// Run a quick exec that will finish first
		_, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "echo", "quick-exec")
		require.NoError(t, err, "quick exec should succeed")

		// Wait for long exec to complete
		<-longExecDone
		wg.Wait()

		// The long exec should have been able to access the SSH agent
		select {
		case result := <-longExecResult:
			got := strings.TrimSpace(result)
			assert.Equal(t, "exit=0", got,
				"Long-running exec should still reach the SSH agent after the quick exec ended (exit 2 = unreachable)")
		default:
			t.Error("Long exec should have completed with a result")
		}
	})
}
