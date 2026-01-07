//go:build e2e

package e2e

import (
	"encoding/json"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInspectCommandE2E tests the dcx inspect command.
func TestInspectCommandE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Test inspect on absent environment
	t.Run("inspect_absent_state", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "inspect")
		assert.Contains(t, stdout, "State:")
		assert.Contains(t, stdout, "ABSENT")
		assert.Contains(t, stdout, "Env Key:")
		assert.Contains(t, stdout, "Plan Type:")
	})

	// Bring up the environment
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test inspect on running environment
	t.Run("inspect_running_state", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "inspect")
		assert.Contains(t, stdout, "State:")
		assert.Contains(t, stdout, "RUNNING")
		assert.Contains(t, stdout, "Container")
		assert.Contains(t, stdout, "Configuration")
	})

	// Test JSON output
	t.Run("inspect_json_output", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "inspect", "--json")

		var result map[string]interface{}
		err := json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "should be valid JSON")

		assert.Equal(t, "RUNNING", result["state"])
		assert.NotEmpty(t, result["env_key"])
		assert.NotEmpty(t, result["config_hash"])
		assert.Equal(t, "single", result["plan_type"])
		assert.NotNil(t, result["container"])
		assert.NotNil(t, result["config"])
	})
}

// TestListCommandE2E tests the dcx list command.
func TestListCommandE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test list shows the environment
	t.Run("list_shows_environment", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "list")
		// Should show at least one environment
		assert.Contains(t, stdout, "RUNNING")
	})

	// Test list with JSON output
	t.Run("list_json_output", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "list", "--json")

		var result []map[string]interface{}
		err := json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "should be valid JSON array")

		// Should have at least one environment
		require.GreaterOrEqual(t, len(result), 1)

		// First result should have expected fields
		env := result[0]
		assert.NotEmpty(t, env["env_key"])
		assert.NotEmpty(t, env["state"])
	})

	// Test list aliases
	t.Run("list_aliases", func(t *testing.T) {
		// Test 'ls' alias
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "ls")
		assert.Contains(t, stdout, "RUNNING")

		// Test 'ps' alias
		stdout = helpers.RunDCXInDirSuccess(t, workspace, "ps")
		assert.Contains(t, stdout, "RUNNING")
	})
}

// TestLogsCommandE2E tests the dcx logs command.
func TestLogsCommandE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := helpers.SimpleImageConfig("alpine:latest")
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Generate some log output
	helpers.RunDCXInDir(t, workspace, "exec", "--", "echo", "test-log-output")

	// Test logs command
	t.Run("logs_shows_output", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "logs")
		// Logs might be empty or have content, but command should succeed
		assert.NoError(t, err)
		_ = stdout // logs content depends on container
	})

	// Test logs with tail
	t.Run("logs_with_tail", func(t *testing.T) {
		_, _, err := helpers.RunDCXInDir(t, workspace, "logs", "--tail", "10")
		assert.NoError(t, err)
	})

	// Test logs with timestamps
	t.Run("logs_with_timestamps", func(t *testing.T) {
		_, _, err := helpers.RunDCXInDir(t, workspace, "logs", "--timestamps")
		assert.NoError(t, err)
	})
}

// TestConfigCommandE2E tests the dcx config command.
func TestConfigCommandE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Config Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"remoteUser": "root",
		"containerEnv": {
			"TEST_VAR": "test-value"
		}
	}`
	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Test config command shows resolved config
	t.Run("config_shows_resolved", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "config")
		assert.Contains(t, stdout, "alpine:latest")
		assert.Contains(t, stdout, "Config Test")
	})

	// Test config with JSON output
	t.Run("config_json_output", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "config", "--json")

		var result map[string]interface{}
		err := json.Unmarshal([]byte(stdout), &result)
		require.NoError(t, err, "should be valid JSON")

		// Check config fields
		config, ok := result["config"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "alpine:latest", config["image"])
		assert.Equal(t, "Config Test", config["name"])
	})

	// Test config --validate-only
	t.Run("config_validate_only", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "config", "--validate-only")
		assert.Contains(t, stdout, "valid")
	})
}

// TestCompletionCommandE2E tests the dcx completion command.
func TestCompletionCommandE2E(t *testing.T) {
	// Test bash completion
	t.Run("completion_bash", func(t *testing.T) {
		stdout := helpers.RunDCXSuccess(t, "completion", "bash")
		assert.Contains(t, stdout, "bash completion")
	})

	// Test zsh completion
	t.Run("completion_zsh", func(t *testing.T) {
		stdout := helpers.RunDCXSuccess(t, "completion", "zsh")
		assert.Contains(t, stdout, "zsh completion") // or compdef
	})

	// Test fish completion
	t.Run("completion_fish", func(t *testing.T) {
		stdout := helpers.RunDCXSuccess(t, "completion", "fish")
		assert.Contains(t, stdout, "fish") // fish completion format
	})
}
