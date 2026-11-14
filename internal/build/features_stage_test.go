package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/features"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStageFeaturesCopiesEachFeatureAndBuiltinEnv(t *testing.T) {
	src := t.TempDir()
	write := func(rel, content string, mode os.FileMode) {
		p := filepath.Join(src, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), mode))
	}
	write("one/install.sh", "#!/bin/sh\necho one\n", 0o755)
	write("one/devcontainer-feature.json", `{"id": "one"}`, 0o644)
	write("one/lib/common.sh", "x=1\n", 0o644)
	write("two/install.sh", "#!/bin/sh\necho two\n", 0o755)
	// Links inside a feature: to a file, to a directory, and dangling
	// (resolved only at install time).
	require.NoError(t, os.Symlink("lib/common.sh", filepath.Join(src, "one", "common.sh")))
	require.NoError(t, os.Symlink("lib", filepath.Join(src, "one", "scripts")))
	require.NoError(t, os.Symlink("/usr/local/share/x", filepath.Join(src, "one", "installed")))

	// A local feature path may itself be a link to the feature directory.
	require.NoError(t, os.Symlink("two", filepath.Join(src, "two-link")))

	dest := t.TempDir()
	list := []*features.Feature{
		{ID: "./one", CachePath: filepath.Join(src, "one")},
		{ID: "./two", CachePath: filepath.Join(src, "two-link")},
	}
	require.NoError(t, stageFeatures(list, dest, "root", "dev"))

	got, err := os.ReadFile(filepath.Join(dest, "feature_0", "lib", "common.sh"))
	require.NoError(t, err)
	assert.Equal(t, "x=1\n", string(got))
	info, err := os.Stat(filepath.Join(dest, "feature_1", "install.sh"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), info.Mode().Perm(), "install.sh stays executable")

	for link, target := range map[string]string{"common.sh": "lib/common.sh", "scripts": "lib", "installed": "/usr/local/share/x"} {
		got, err := os.Readlink(filepath.Join(dest, "feature_0", link))
		require.NoError(t, err, "%s is staged as a link", link)
		assert.Equal(t, target, got)
	}

	env, err := os.ReadFile(filepath.Join(dest, "builtin.env"))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(env), "_REMOTE_USER=dev") || strings.Contains(string(env), `_REMOTE_USER="dev"`),
		"builtin.env names the remote user:\n%s", env)
}

func TestStageFeaturesFailsOnAMissingFeature(t *testing.T) {
	err := stageFeatures([]*features.Feature{{ID: "./gone", CachePath: filepath.Join(t.TempDir(), "gone")}}, t.TempDir(), "root", "dev")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "./gone")
}
