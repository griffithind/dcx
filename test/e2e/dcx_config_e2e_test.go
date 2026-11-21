//go:build e2e

package e2e

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDcxConfigProjectNameE2E: the devcontainer's name gives the SSH alias,
// the compose project (<name>-<id8>), the container name and the label.
func TestDcxConfigProjectNameE2E(t *testing.T) {
	t.Parallel()
	env := composeEnv(t)
	slug := devcontainer.SanitizeProjectName(env.Name)
	alias := slug + ".dcx"
	assert.Contains(t, env.UpOutput, "SSH configured: ssh "+alias)

	st := helpers.GetStatus(t, env.Dir)
	assert.Equal(t, alias, st.SSHAlias)
	project := st.ResourceName
	require.True(t, strings.HasPrefix(project, slug+"-"), "project = %q", project)
	assert.Equal(t, project+"-app-1", helpers.ContainerName(t, env.Dir))

	out, err := exec.Command("docker", "inspect", "--format",
		`{{index .Config.Labels "com.griffithind.dcx.compose.project"}}`, project+"-app-1").CombinedOutput()
	require.NoError(t, err, "failed to inspect container: %s", out)
	assert.Equal(t, project, strings.TrimSpace(string(out)))
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
