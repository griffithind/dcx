package container

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDockerCLI puts a `docker` on PATH that logs its arguments and
// answers `image inspect` with one image.
func fakeDockerCLI(t *testing.T) (logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell")
	}
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls")
	script := `#!/bin/sh
echo "$*" >> "` + logPath + `"
case "$1 $2" in
"image inspect") echo '[{"Id":"sha256:abc","Config":{"User":"dev","Labels":{"k":"v"}}}]' ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func calls(t *testing.T, logPath string) []string {
	t.Helper()
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return nil
	}
	require.NoError(t, err)
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// An up asks about the same image several times (metadata, ID, config,
// existence): docker is asked once, until a pull or build may have moved
// the tag.
func TestImageInspectIsCachedUntilImagesChange(t *testing.T) {
	log := fakeDockerCLI(t)
	d := NewDockerCLI()
	ctx := context.Background()

	id, err := d.ImageID(ctx, "base:1")
	require.NoError(t, err)
	assert.Equal(t, "sha256:abc", id)
	labels, err := d.ImageLabels(ctx, "base:1")
	require.NoError(t, err)
	assert.Equal(t, "v", labels["k"])
	cfg, err := d.ImageConfig(ctx, "base:1")
	require.NoError(t, err)
	assert.Equal(t, "dev", cfg.User)
	exists, err := d.ImageExists(ctx, "base:1")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Len(t, calls(t, log), 1, "one inspect for four questions")

	require.NoError(t, d.PullImageWithProgress(ctx, "base:1", nil))
	_, err = d.ImageID(ctx, "base:1")
	require.NoError(t, err)
	assert.Len(t, calls(t, log), 3, "a pull clears the cache: pull, then inspect again")
}
