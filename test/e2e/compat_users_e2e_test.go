//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestImageUserE2E: with no remoteUser, the image's USER gets the UID
// update (M2), sessions run as it by name with its home (M13), and the
// container's SHELL is the session shell (M4).
func TestImageUserE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	if os.Getuid() == 0 {
		t.Skip("the UID update never applies to a root host user")
	}

	ws := helpers.CreateWorkspaceFiles(t, map[string]string{
		".devcontainer/devcontainer.json": fmt.Sprintf(`{"name": %q, "build": {"dockerfile": "Dockerfile"}}`, helpers.UniqueTestName(t)),
		".devcontainer/Dockerfile":        "FROM alpine:3.24\nRUN adduser -D -u 4321 app\nENV SHELL=/bin/ash\nUSER app\n",
	})
	helpers.RunDCXInDirSuccess(t, ws, "up")

	stdout, stderr, err := helpers.RunDCXInDir(t, ws, "ssh", "--", "sh", "-c", "id -u; echo $USER; echo $HOME; echo $SHELL")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, []string{strconv.Itoa(os.Getuid()), "app", "/home/app", "/bin/ash"}, strings.Fields(stdout))
}

// TestInitializeCommandRunsOnEveryUpE2E: initializeCommand runs on every
// up, also when the container is already running (M6).
func TestInitializeCommandRunsOnEveryUpE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"initializeCommand": "echo ran >> initialize.log"
	}`, helpers.UniqueTestName(t)))
	helpers.RunDCXInDirSuccess(t, ws, "up")
	helpers.RunDCXInDirSuccess(t, ws, "up")

	data, err := os.ReadFile(filepath.Join(ws, "initialize.log"))
	require.NoError(t, err)
	assert.Equal(t, "ran\nran\n", string(data))
}
