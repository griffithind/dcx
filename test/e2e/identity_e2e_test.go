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

// containerID returns the ID of the workspace's primary container.
func containerID(t *testing.T, workspace string) string {
	t.Helper()
	name := helpers.ContainerName(t, workspace)
	require.NotEmpty(t, name)
	out, err := exec.Command("docker", "inspect", "--format", "{{.Id}}", name).Output()
	require.NoError(t, err)
	return strings.TrimSpace(string(out))
}

// Edits that don't affect the container must not recreate it; edits that
// do must.
func TestConfigEditsRecreateOnlyWhenNeededE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	name := helpers.UniqueTestName(t)
	write := func(ws, body string) {
		require.NoError(t, os.WriteFile(filepath.Join(ws, ".devcontainer", "devcontainer.json"), []byte(body), 0o644))
	}
	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{"name": %q, "image": "alpine:latest", "containerEnv": {"A": "1"}}`, name))
	t.Cleanup(func() { helpers.RunDCXInDir(t, ws, "down") })

	helpers.RunDCXInDirSuccess(t, ws, "up")
	first := containerID(t, ws)

	write(ws, fmt.Sprintf(`{
		// only a comment, formatting and editor settings changed
		"image": "alpine:latest",
		"name": %q,
		"containerEnv": {"A": "1"},
		"customizations": {"vscode": {"extensions": ["golang.go"]}},
	}`, name))
	out := helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Contains(t, out, "already running")
	assert.Equal(t, first, containerID(t, ws), "cosmetic edits must not recreate the container")

	write(ws, fmt.Sprintf(`{"name": %q, "image": "alpine:latest", "containerEnv": {"A": "2"}}`, name))
	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.NotEqual(t, first, containerID(t, ws), "a containerEnv change must recreate")
	assert.Equal(t, "2", dcxExec(t, ws, "printenv", "A"))
}

// A cached features image must still get the UID update layer (it used to
// be skipped on cache hits), and editing a local feature must rebuild it.
func TestFeaturesUIDAndLocalFeatureEditsE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	hostUID := os.Getuid()
	if hostUID == 0 || hostUID == 4321 {
		t.Skip("needs a non-root host user whose UID differs from the image user's")
	}

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"remoteUser": "dev",
		"features": {"./marker": {}}
	}`, helpers.UniqueTestName(t)))
	dc := filepath.Join(ws, ".devcontainer")
	require.NoError(t, os.WriteFile(filepath.Join(dc, "Dockerfile"), []byte("FROM alpine:latest\nRUN adduser -D -u 4321 dev\n"), 0o644))
	feat := filepath.Join(dc, "marker")
	require.NoError(t, os.MkdirAll(feat, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(feat, "devcontainer-feature.json"), []byte(`{"id": "marker", "version": "1.0.0"}`), 0o644))
	writeInstall := func(v string) {
		require.NoError(t, os.WriteFile(filepath.Join(feat, "install.sh"), []byte("#!/bin/sh\necho "+v+" > /marker\n"), 0o755))
	}
	writeInstall("v1")
	t.Cleanup(func() { helpers.RunDCXInDir(t, ws, "down") })

	want := strconv.Itoa(hostUID)
	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Equal(t, want, dcxExec(t, ws, "id", "-u", "dev"))
	assert.Equal(t, "v1", dcxExec(t, ws, "cat", "/marker"))

	// down + up reuses the cached images; the UID layer must still apply.
	helpers.RunDCXInDirSuccess(t, ws, "down")
	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Equal(t, want, dcxExec(t, ws, "id", "-u", "dev"), "UID layer missing after cache hit")

	// Editing the local feature must be detected and rebuilt.
	writeInstall("v2")
	helpers.RunDCXInDirSuccess(t, ws, "up")
	assert.Equal(t, "v2", dcxExec(t, ws, "cat", "/marker"))
	assert.Equal(t, want, dcxExec(t, ws, "id", "-u", "dev"))
}

// `dcx clean` must remove dcx-built images that no container uses (it used
// to match a repository name dcx never produced). The test uses --current so
// it never removes the developer's own images.
func TestCleanRemovesUnusedDcxImagesE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	ws := helpers.CreateTempWorkspace(t, fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace"
	}`, helpers.UniqueTestName(t)))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".devcontainer", "Dockerfile"), []byte("FROM alpine:latest\nRUN echo clean-test > /x\n"), 0o644))
	t.Cleanup(func() { helpers.RunDCXInDir(t, ws, "down") })

	helpers.RunDCXInDirSuccess(t, ws, "up")
	id := containerID(t, ws)
	img, err := exec.Command("docker", "inspect", "--format", "{{.Config.Image}}", id).Output()
	require.NoError(t, err)
	image := strings.TrimSpace(string(img))
	require.True(t, strings.HasPrefix(image, "dcx/"), "image %q", image)

	// In use: kept.
	helpers.RunDCXInDirSuccess(t, ws, "clean", "--current")
	require.NoError(t, exec.Command("docker", "image", "inspect", image).Run(), "in-use image must be kept")

	// Unused after down: removed.
	helpers.RunDCXInDirSuccess(t, ws, "down")
	helpers.RunDCXInDirSuccess(t, ws, "clean", "--current")
	assert.Error(t, exec.Command("docker", "image", "inspect", image).Run(), "unused dcx image must be removed")
}
