//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingFeature is a local feature whose install.sh writes what it sees
// to /opt/rec/<name>.
func recordingFeature(name, metadata, record string) map[string]string {
	return map[string]string{
		".devcontainer/" + name + "/devcontainer-feature.json": metadata,
		".devcontainer/" + name + "/install.sh":                "#!/bin/sh\nset -e\nmkdir -p /opt/rec\necho \"" + record + "\" > /opt/rec/" + name + "\n",
	}
}

// TestFeatureInstallEnvironmentE2E builds features on an image whose USER is
// not root: they install as root whatever the USER (needs-root records its
// uid), the image's USER is restored afterwards, features install for the
// image's user when there is no containerUser (F3), a feature's
// containerEnv is set before its install.sh runs (F4), and its options
// reach only its own install.sh (F5); installsAfter orders first and
// second. With no remoteUser, the image's USER gets the UID update (M2),
// sessions run as it by name with its home (M13), and the container's SHELL
// is the session shell (M4).
func TestFeatureInstallEnvironmentE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	if os.Getuid() == 0 || os.Getuid() == 4321 {
		t.Skip("needs a non-root host user whose UID differs from the image user's")
	}

	files := map[string]string{
		".devcontainer/devcontainer.json": fmt.Sprintf(`{
			"name": %q,
			"build": {"dockerfile": "Dockerfile"},
			"workspaceFolder": "/workspace",
			"features": {"./first": {"greeting": "hi there"}, "./second": {}, "./needs-root": {}}
		}`, helpers.UniqueTestName(t)),
		".devcontainer/Dockerfile":                           "FROM alpine:3.24\nRUN adduser -D -u 4321 app\nENV SHELL=/bin/ash\nUSER app\n",
		".devcontainer/needs-root/devcontainer-feature.json": `{"id": "needs-root", "version": "1.0.0", "name": "Needs Root"}`,
		".devcontainer/needs-root/install.sh":                "#!/bin/sh\nset -e\nid -u > /usr/local/share/installed-as\n",
	}
	for k, v := range recordingFeature("first",
		`{"id": "first", "options": {"greeting": {"type": "string", "default": "hello"}}, "containerEnv": {"FIRST_HOME": "/opt/first"}}`,
		`$_CONTAINER_USER|$_REMOTE_USER|$GREETING|$FIRST_HOME`) {
		files[k] = v
	}
	for k, v := range recordingFeature("second", `{"id": "second", "installsAfter": ["./first"]}`, `${GREETING:-unset}|$FIRST_HOME`) {
		files[k] = v
	}
	ws := helpers.CreateWorkspaceFiles(t, files)
	require.NoError(t, os.Chmod(filepath.Join(ws, ".devcontainer", "needs-root", "install.sh"), 0o755))
	helpers.RunDCXInDirSuccess(t, ws, "up")

	stdout, stderr, err := helpers.RunDCXInDir(t, ws, "ssh", "--", "cat", "/opt/rec/first", "/opt/rec/second")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, []string{"app|app|hi there|/opt/first", "unset|/opt/first"}, strings.Split(strings.TrimSpace(stdout), "\n"),
		"first sees the image user, its option and its containerEnv; second sees no option of first")
	assert.Equal(t, "0", dcxExec(t, ws, "cat", "/usr/local/share/installed-as"), "the feature installed as root")
	out, err := exec.Command("docker", "inspect", "--format", "{{.Config.User}}", helpers.ContainerName(t, ws)).Output()
	require.NoError(t, err)
	assert.Equal(t, "app", strings.TrimSpace(string(out)), "the image's USER is restored")

	stdout, stderr, err = helpers.RunDCXInDir(t, ws, "ssh", "--", "sh", "-c", "id -u; echo $USER; echo $HOME; echo $SHELL")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, []string{strconv.Itoa(os.Getuid()), "app", "/home/app", "/bin/ash"}, strings.Fields(stdout))
}
