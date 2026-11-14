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

// TestDcxConfigProjectNameE2E tests that the project name from devcontainer.json name field is used for container naming.
func TestDcxConfigProjectNameE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "testproject",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace"
	}`

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:3.24
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	// Test dcx up with project name
	t.Run("up_uses_project_name", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Devcontainer started successfully")
		// SSH should be configured with project name
		assert.Contains(t, stdout, "SSH configured: ssh testproject.dcx")
	})

	// Test status shows project name
	t.Run("status_shows_project_name", func(t *testing.T) {
		st := helpers.GetStatus(t, workspace)
		assert.Contains(t, st.ResourceName, "testproject", "resource name comes from the devcontainer name")
		assert.Equal(t, "testproject.dcx", st.SSHAlias)
	})

	// The compose project is "<name>-<workspace id prefix>" so two checkouts
	// with the same name don't share containers or volumes.
	project := helpers.GetStatus(t, workspace).ResourceName

	t.Run("container_name_uses_project", func(t *testing.T) {
		require.True(t, strings.HasPrefix(project, "testproject-"), "project = %q", project)
		assert.Equal(t, project+"-app-1", helpers.ContainerName(t, workspace))
	})

	// Test compose_project label is set correctly
	t.Run("compose_project_label", func(t *testing.T) {
		cmd := exec.Command("docker", "inspect", "--format",
			`{{index .Config.Labels "com.griffithind.dcx.compose.project"}}`,
			project+"-app-1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "failed to inspect container: %s", output)

		labelValue := strings.TrimSpace(string(output))
		assert.Equal(t, project, labelValue)
	})
}

// TestDcxConfigShortcutsE2E tests the shortcuts functionality from customizations.dcx.
func TestDcxConfigShortcutsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "shortcutstest",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"shortcuts": {
					"hello": "echo hello from shortcut",
					"greet": {"command": "echo greeting", "description": "Say hello"},
					"say": {"prefix": "echo", "passArgs": true, "description": "Echo with args"}
				}
			}
		}
	}`

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:3.24
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	// Bring up the environment
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Test status shows shortcuts count
	t.Run("status_shows_shortcuts_count", func(t *testing.T) {
		assert.Equal(t, 3, helpers.GetStatus(t, workspace).Shortcuts)
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

// TestDcxConfigFlagPassthroughE2E tests that flags are passed through to shortcuts.
func TestDcxConfigFlagPassthroughE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := `{
		"name": "flagtest",
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"shortcuts": {
					"echoflags": {"prefix": "echo", "passArgs": true}
				}
			}
		}
	}`

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:3.24
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

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
		"name": "noenvtest",
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"shortcuts": {
					"test": "echo test"
				}
			}
		}
	}`

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Don't bring up, just check status
	t.Run("status_shows_project_and_shortcuts_when_absent", func(t *testing.T) {
		st := helpers.GetStatus(t, workspace)
		assert.Contains(t, st.ResourceName, "noenvtest")
		assert.Equal(t, "absent", st.State)
		assert.Equal(t, 1, st.Shortcuts)
		assert.Empty(t, st.SSHAlias, "no SSH alias before the environment exists")
	})
}
