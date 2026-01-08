//go:build e2e

package e2e

import (
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecE2E tests exec functionality with a shared container.
// This consolidates basic exec, shell commands, and exit code tests.
func TestExecE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig(t, "alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container once for all subtests
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Basic exec tests
	t.Run("basic", func(t *testing.T) {
		t.Run("simple_echo", func(t *testing.T) {
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "hello")
			require.NoError(t, err)
			assert.Contains(t, stdout, "hello")
		})

		t.Run("multiple_args", func(t *testing.T) {
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "one", "two", "three")
			require.NoError(t, err)
			assert.Contains(t, stdout, "one two three")
		})

		t.Run("pwd", func(t *testing.T) {
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "pwd")
			require.NoError(t, err)
			assert.Contains(t, stdout, "/workspace")
		})
	})

	// Shell command tests
	t.Run("shell_commands", func(t *testing.T) {
		t.Run("sh_c_echo", func(t *testing.T) {
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo nested_test")
			require.NoError(t, err)
			assert.Contains(t, stdout, "nested_test")
		})

		t.Run("sh_c_multiple_commands", func(t *testing.T) {
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo one && echo two")
			require.NoError(t, err)
			assert.Contains(t, stdout, "one")
			assert.Contains(t, stdout, "two")
		})

		t.Run("sh_c_pipe", func(t *testing.T) {
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo hello_world | tr '_' ' '")
			require.NoError(t, err)
			assert.Contains(t, stdout, "hello world")
		})

		t.Run("sh_c_variable", func(t *testing.T) {
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "VAR=test123; echo $VAR")
			require.NoError(t, err)
			assert.Contains(t, stdout, "test123")
		})

		t.Run("sh_c_semicolon", func(t *testing.T) {
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo a; echo b; echo c")
			require.NoError(t, err)
			assert.Contains(t, stdout, "a")
			assert.Contains(t, stdout, "b")
			assert.Contains(t, stdout, "c")
		})

		t.Run("bash_c_quotes", func(t *testing.T) {
			stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo \"it's working\"")
			require.NoError(t, err)
			assert.Contains(t, stdout, "it's working")
		})
	})

	// Exit code tests
	t.Run("exit_codes", func(t *testing.T) {
		t.Run("exit_0", func(t *testing.T) {
			_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "exit 0")
			require.NoError(t, err)
		})

		t.Run("exit_1", func(t *testing.T) {
			_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "exit 1")
			require.Error(t, err)
		})

		t.Run("false_command", func(t *testing.T) {
			_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "false")
			require.Error(t, err)
		})

		t.Run("true_command", func(t *testing.T) {
			_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "true")
			require.NoError(t, err)
		})
	})
}

// TestExecWithEnvE2E tests exec with environment variables from config.
// This needs its own container due to the different containerEnv configuration.
func TestExecWithEnvE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Exec Env Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"containerEnv": {
			"MY_TEST_VAR": "exec-test-value"
		}
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test env var is accessible
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "MY_TEST_VAR")
	require.NoError(t, err)
	assert.Contains(t, stdout, "exec-test-value")
}
