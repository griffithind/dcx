//go:build e2e

package e2e

import (
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecBasicE2E tests basic exec functionality.
func TestExecBasicE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test simple exec
	t.Run("simple_echo", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "hello")
		require.NoError(t, err)
		assert.Contains(t, stdout, "hello")
	})

	// Test exec with multiple args
	t.Run("multiple_args", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "one", "two", "three")
		require.NoError(t, err)
		assert.Contains(t, stdout, "one two three")
	})

	// Test exec pwd
	t.Run("pwd", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "pwd")
		require.NoError(t, err)
		assert.Contains(t, stdout, "/workspace")
	})
}

// TestExecShellCommandsE2E tests exec with shell commands.
func TestExecShellCommandsE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test exec with sh -c
	t.Run("sh_c_echo", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo nested_test")
		require.NoError(t, err)
		assert.Contains(t, stdout, "nested_test")
	})

	// Test exec with sh -c and multiple commands
	t.Run("sh_c_multiple_commands", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo one && echo two")
		require.NoError(t, err)
		assert.Contains(t, stdout, "one")
		assert.Contains(t, stdout, "two")
	})

	// Test exec with pipe
	t.Run("sh_c_pipe", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo hello_world | tr '_' ' '")
		require.NoError(t, err)
		assert.Contains(t, stdout, "hello world")
	})

	// Test exec with variable
	t.Run("sh_c_variable", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "VAR=test123; echo $VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "test123")
	})

	// Test exec with semicolon
	t.Run("sh_c_semicolon", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo a; echo b; echo c")
		require.NoError(t, err)
		assert.Contains(t, stdout, "a")
		assert.Contains(t, stdout, "b")
		assert.Contains(t, stdout, "c")
	})

	// Test bash -c with quotes in string
	t.Run("bash_c_quotes", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "echo \"it's working\"")
		require.NoError(t, err)
		assert.Contains(t, stdout, "it's working")
	})
}

// TestExecExitCodeE2E tests that exec returns correct exit codes.
func TestExecExitCodeE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test exit 0
	t.Run("exit_0", func(t *testing.T) {
		_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "exit 0")
		require.NoError(t, err)
	})

	// Test exit 1
	t.Run("exit_1", func(t *testing.T) {
		_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "sh", "-c", "exit 1")
		require.Error(t, err)
	})

	// Test false command
	t.Run("false_command", func(t *testing.T) {
		_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "false")
		require.Error(t, err)
	})

	// Test true command
	t.Run("true_command", func(t *testing.T) {
		_, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "true")
		require.NoError(t, err)
	})
}

// TestExecWithEnvE2E tests exec with environment variables from config.
func TestExecWithEnvE2E(t *testing.T) {
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
	t.Run("env_var_accessible", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "printenv", "MY_TEST_VAR")
		require.NoError(t, err)
		assert.Contains(t, stdout, "exec-test-value")
	})
}
