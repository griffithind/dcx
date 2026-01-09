//go:build e2e

package e2e

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDcxConfigProjectNameE2E tests that the project name from dcx.json is used for container naming.
func TestDcxConfigProjectNameE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "Project Name Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	dcxJSON := `{
		"name": "testproject",
		"up": {
			"ssh": true
		}
	}`

	workspace := helpers.CreateTempComposeWorkspaceWithDcx(t, devcontainerJSON, dockerComposeYAML, dcxJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Test dcx up with project name
	t.Run("up_uses_project_name", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Devcontainer started successfully")
		// SSH should be configured with project name
		assert.Contains(t, stdout, "SSH configured: ssh testproject.dcx")
	})

	// Test status shows project name
	t.Run("status_shows_project_name", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.True(t, helpers.ContainsLabel(stdout, "Project", "testproject"), "should show project name")
		assert.True(t, helpers.ContainsLabel(stdout, "SSH", "ssh testproject.dcx"), "should show SSH command")
	})

	// Test container name uses project name
	t.Run("container_name_uses_project", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		// Container name should be testproject-app-1, not dcx_<hash>-app-1
		assert.Contains(t, stdout, "Name:")
		assert.Contains(t, stdout, "testproject-app-1")
	})

	// Test compose_project label is set correctly
	t.Run("compose_project_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.compose.project"}}`,
			"testproject-app-1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, "testproject", labelValue)
	})
}

// TestDcxConfigShortcutsE2E tests the shortcuts functionality from dcx.json.
func TestDcxConfigShortcutsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "Shortcuts Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	dcxJSON := `{
		"name": "shortcutstest",
		"shortcuts": {
			"hello": "echo hello from shortcut",
			"greet": {"command": "echo greeting", "description": "Say hello"},
			"say": {"prefix": "echo", "passArgs": true, "description": "Echo with args"}
		}
	}`

	workspace := helpers.CreateTempComposeWorkspaceWithDcx(t, devcontainerJSON, dockerComposeYAML, dcxJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the environment
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test dcx run --list
	t.Run("run_list_shows_shortcuts", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "run", "--list")
		assert.Contains(t, stdout, "Available shortcuts:")
		assert.Contains(t, stdout, "hello")
		assert.Contains(t, stdout, "greet")
		assert.Contains(t, stdout, "say")
		assert.Contains(t, stdout, "Say hello") // description
		assert.Contains(t, stdout, "Echo with args") // description
	})

	// Test status shows shortcuts count
	t.Run("status_shows_shortcuts_count", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.True(t, helpers.ContainsLabel(stdout, "Shortcuts", "3 defined"), "should show shortcuts count")
	})

	// Test running simple command shortcut
	t.Run("run_simple_command_shortcut", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "run", "hello")
		require.NoError(t, err)
		assert.Contains(t, stdout, "hello from shortcut")
	})

	// Test running command object shortcut
	t.Run("run_command_object_shortcut", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "run", "greet")
		require.NoError(t, err)
		assert.Contains(t, stdout, "greeting")
	})

	// Test running prefix shortcut with args
	t.Run("run_prefix_shortcut_with_args", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "run", "say", "hello", "world")
		require.NoError(t, err)
		assert.Contains(t, stdout, "hello world")
	})

	// Test running prefix shortcut without args
	t.Run("run_prefix_shortcut_without_args", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "run", "say")
		require.NoError(t, err)
		// Just "echo" with no args outputs empty line
		assert.Equal(t, "\n", stdout)
	})

	// Test unknown shortcut
	t.Run("run_unknown_shortcut_fails", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "run", "nonexistent")
		require.Error(t, err)
		assert.Contains(t, stderr, "unknown shortcut")
	})
}

// TestDcxConfigUpOptionsE2E tests that up options from dcx.json are respected.
func TestDcxConfigUpOptionsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "Up Options Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	// Test with ssh: true in dcx.json
	dcxJSON := `{
		"name": "upoptionstest",
		"up": {
			"ssh": true
		}
	}`

	workspace := helpers.CreateTempComposeWorkspaceWithDcx(t, devcontainerJSON, dockerComposeYAML, dcxJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up - should enable SSH automatically from dcx.json
	t.Run("up_respects_ssh_option", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "SSH configured:")
	})

	// Status should show SSH is configured
	t.Run("status_shows_ssh_enabled", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.Contains(t, stdout, "SSH:")
		assert.Contains(t, stdout, "ssh upoptionstest.dcx")
	})
}

// TestDcxConfigMigrationE2E tests that existing containers are still found after adding dcx.json.
func TestDcxConfigMigrationE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "Migration Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	// Start without dcx.json
	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Create environment without dcx.json
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Get status before adding dcx.json
	statusBefore := helpers.RunDCXInDirSuccess(t, workspace, "status")
	assert.True(t, helpers.ContainsLabel(statusBefore, "State", "RUNNING"), "should be running")
	// Container uses compose-style naming (project-service-instance)
	assert.Contains(t, statusBefore, "-app-")

	// Now add dcx.json with a project name
	dcxJSON := `{
		"name": "migrationtest"
	}`

	// Write dcx.json
	dcxPath := workspace + "/.devcontainer/dcx.json"
	require.NoError(t, exec.Command("sh", "-c", "echo '"+dcxJSON+"' > "+dcxPath).Run())

	// Status should still find the container (labels are used for lookup, not name)
	t.Run("status_finds_old_container_after_adding_dcx_json", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.True(t, helpers.ContainsLabel(stdout, "State", "RUNNING"), "should be running")
		assert.True(t, helpers.ContainsLabel(stdout, "Project", "migrationtest"), "should show project name")
		// Container name still has compose naming
		assert.Contains(t, stdout, "-app-")
	})

	// Stop should work with migration support
	t.Run("stop_works_with_migration", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "stop")
		assert.Contains(t, stdout, "stopped")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "CREATED", state)
	})

	// Up should work with migration support (start stopped container)
	t.Run("up_works_with_migration", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "started")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// Down should work with migration support
	t.Run("down_works_with_migration", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "down")
		assert.Contains(t, stdout, "removed")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "ABSENT", state)
	})
}

// TestDcxConfigFlagPassthroughE2E tests that flags are passed through to shortcuts.
func TestDcxConfigFlagPassthroughE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "Flag Passthrough Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	dcxJSON := `{
		"name": "flagtest",
		"shortcuts": {
			"echoflags": {"prefix": "echo", "passArgs": true}
		}
	}`

	workspace := helpers.CreateTempComposeWorkspaceWithDcx(t, devcontainerJSON, dockerComposeYAML, dcxJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test that flags like --version are passed through to the command
	t.Run("flags_pass_through_to_command", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "run", "echoflags", "--flag1", "-f", "value")
		require.NoError(t, err)
		assert.Contains(t, stdout, "--flag1")
		assert.Contains(t, stdout, "-f")
		assert.Contains(t, stdout, "value")
	})
}

// TestDcxConfigStatusWithoutEnvironmentE2E tests status output when no environment exists.
func TestDcxConfigStatusWithoutEnvironmentE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "Status No Env Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace"
	}`

	dcxJSON := `{
		"name": "noenvtest",
		"shortcuts": {
			"test": "echo test"
		}
	}`

	workspace := helpers.CreateTempWorkspaceWithDcx(t, devcontainerJSON, dcxJSON)

	// Don't bring up, just check status
	t.Run("status_shows_project_and_shortcuts_when_absent", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.True(t, helpers.ContainsLabel(stdout, "Project", "noenvtest"), "should show project name")
		assert.True(t, helpers.ContainsLabel(stdout, "State", "ABSENT"), "should show ABSENT state")
		assert.True(t, helpers.ContainsLabel(stdout, "Shortcuts", "1 defined"), "should show shortcuts count")
		// SSH should NOT be shown when environment is absent
		assert.NotContains(t, helpers.StripANSI(stdout), "SSH:")
	})
}

// TestDcxConfigNoShortcutsE2E tests run command when no shortcuts are defined.
func TestDcxConfigNoShortcutsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "No Shortcuts Test",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	// dcx.json with name but no shortcuts
	dcxJSON := `{
		"name": "noshortcuts"
	}`

	workspace := helpers.CreateTempComposeWorkspaceWithDcx(t, devcontainerJSON, dockerComposeYAML, dcxJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	t.Run("run_list_with_no_shortcuts", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "run", "--list")
		assert.Contains(t, stdout, "No shortcuts defined")
	})

	t.Run("run_shortcut_fails_with_no_shortcuts", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "run", "anything")
		require.Error(t, err)
		assert.Contains(t, stderr, "no shortcuts defined")
	})

	t.Run("status_does_not_show_shortcuts_line", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "status")
		assert.NotContains(t, stdout, "Shortcuts:")
	})
}
