package devcontainer

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The compose model is docker compose's: paths are already resolved, so
// the base image and the Dockerfiles come from it as written.
func TestComposeConfigBaseImageAndDockerfiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app", "Dockerfile.dev"), "ARG V=1-20\nFROM mcr.microsoft.com/devcontainers/javascript-node:${V}\n")
	cfg, err := ParseComposeConfig([]byte(`{"name": "proj", "services": {
		"app": {"image": "ignored-when-built", "build": {"context": "` + filepath.Join(dir, "app") + `", "dockerfile": "Dockerfile.dev", "args": {"V": "1-22"}}},
		"inline": {"build": {"context": "` + dir + `", "dockerfile_inline": "FROM alpine"}},
		"remote": {"build": {"context": "https://github.com/example/repo.git"}},
		"db": {"image": "postgres:16", "entrypoint": null, "command": null}}}`))
	require.NoError(t, err)

	assert.Equal(t, "mcr.microsoft.com/devcontainers/javascript-node:1-22", cfg.baseImage("app"), "a build's FROM, with its args")
	assert.Equal(t, "postgres:16", cfg.baseImage("db"))
	assert.Empty(t, cfg.baseImage("inline"), "no Dockerfile to read")
	assert.Empty(t, cfg.baseImage("missing"))
	assert.Equal(t, []string{filepath.Join(dir, "app", "Dockerfile.dev")}, cfg.Dockerfiles(), "only local Dockerfiles")
}

// An override file in another folder builds relative to the project
// directory, as compose resolves it (the review's P2): metadata discovery
// and the staleness key both follow the model.
func TestComposeOverrideInAnotherFolder(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "proj")
	copyTree(t, "testdata/resolve/compose-override-dir/workspace", ws)
	opts := ResolveOptions{
		WorkspacePath: ws,
		Images:        goldenImages("testdata/resolve/compose-override-dir"),
		Compose:       goldenCompose{"testdata/resolve/compose-override-dir", ws},
	}
	r, err := ResolveWorkspace(t.Context(), opts)
	require.NoError(t, err)
	assert.Equal(t, "mcr.example/base:1", r.MetadataImage)
	assert.Equal(t, "dev", r.RemoteUser, "the base image's metadata applies")

	writeFile(t, filepath.Join(ws, ".devcontainer", "Dockerfile.dev"), "FROM mcr.example/base:2\n")
	edited, err := ResolveWorkspace(t.Context(), opts)
	require.NoError(t, err)
	assert.NotEqual(t, r.ConfigHash, edited.ConfigHash, "editing the Dockerfile marks the container stale")
}

func TestComposeConfigNeedsASource(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "proj")
	copyTree(t, "testdata/resolve/compose/workspace", ws)
	_, err := ResolveWorkspace(t.Context(), ResolveOptions{WorkspacePath: ws})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "docker compose")
}
