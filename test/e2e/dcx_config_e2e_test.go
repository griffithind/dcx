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
    init: true
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

// TestDcxConfigStatusWithoutEnvironmentE2E tests status output when no environment exists.
func TestDcxConfigStatusWithoutEnvironmentE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := `{
		"name": "noenvtest",
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace"
	}`

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	// Don't bring up, just check status
	t.Run("status_shows_project_when_absent", func(t *testing.T) {
		st := helpers.GetStatus(t, workspace)
		assert.Contains(t, st.ResourceName, "noenvtest")
		assert.Equal(t, "absent", st.State)
		assert.Empty(t, st.SSHAlias, "no SSH alias before the environment exists")
	})
}
