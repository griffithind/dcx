//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDockerfileBaseImageMetadataE2E guards the metadata of the image a
// Dockerfile builds FROM (S1) and the entrypoints recorded in it (F6): the
// base image's remoteUser, hooks and entrypoint used to be ignored for
// Dockerfile configs, so sessions ran as root and its init never ran.
func TestDockerfileBaseImageMetadataE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	// A local "prebuilt" image: user app, an init script, and a
	// devcontainer.metadata label pointing at both.
	baseTag := "dcx-e2e-meta:" + strings.ToLower(helpers.UniqueTestName(t))
	baseDir := t.TempDir()
	label := `[{"remoteUser": "app", "postCreateCommand": "echo from-base > /tmp/base-hook"}, {"id": "local/init", "entrypoint": "/usr/local/bin/base-init.sh"}]`
	baseDockerfile := fmt.Sprintf(`FROM alpine:3.24
RUN adduser -D app && printf '#!/bin/sh\necho ran > /tmp/base-init\nexec "$@"\n' > /usr/local/bin/base-init.sh && chmod +x /usr/local/bin/base-init.sh
LABEL devcontainer.metadata=%q
`, label)
	require.NoError(t, os.WriteFile(filepath.Join(baseDir, "Dockerfile"), []byte(baseDockerfile), 0o644))
	out, err := exec.Command("docker", "build", "-q", "-t", baseTag, baseDir).CombinedOutput()
	require.NoError(t, err, string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "image", "rm", "-f", baseTag).Run() })

	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".devcontainer")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("ARG BASE\nFROM ${BASE}\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile", "args": {"BASE": %q}},
		"workspaceFolder": "/workspace",
		"updateRemoteUserUID": false
	}`, helpers.UniqueTestName(t), baseTag)), 0o644))
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down") })

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "id", "-un")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, "app", strings.TrimSpace(stdout), "remoteUser comes from the base image's metadata")

	stdout, stderr, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/base-hook", "/tmp/base-init")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, "from-base\nran", strings.TrimSpace(stdout), "the base image's hook and entrypoint ran")
}
