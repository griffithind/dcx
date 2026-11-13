package features

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type tarEntry struct {
	name     string
	typ      byte
	body     string
	linkname string
	mode     int64
}

func buildTar(t *testing.T, entries []tarEntry) *tar.Reader {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0644
		}
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: mode, Linkname: e.linkname, Size: int64(len(e.body))}
		require.NoError(t, tw.WriteHeader(hdr))
		if e.typ == tar.TypeReg {
			_, err := tw.Write([]byte(e.body))
			require.NoError(t, err)
		}
	}
	require.NoError(t, tw.Close())
	return tar.NewReader(&buf)
}

func TestExtractTarExtractsRegularFeature(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "feature")
	tr := buildTar(t, []tarEntry{
		{name: "./", typ: tar.TypeDir},
		{name: "./devcontainer-feature.json", typ: tar.TypeReg, body: `{"id":"x"}`},
		{name: "./install.sh", typ: tar.TypeReg, body: "#!/bin/sh\n", mode: 0755},
		{name: "./lib/", typ: tar.TypeDir},
		{name: "./lib/helper.sh", typ: tar.TypeReg, body: "echo"},
		{name: "./run.sh", typ: tar.TypeSymlink, linkname: "install.sh"},
	})
	require.NoError(t, extractTarReader(tr, dest))

	data, err := os.ReadFile(filepath.Join(dest, "run.sh"))
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\n", string(data))
}

// A symlink pointing outside the cache followed by an entry written through
// it must not write outside the destination.
func TestExtractTarRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "outside")
	require.NoError(t, os.MkdirAll(outside, 0755))
	dest := filepath.Join(root, "feature")
	require.NoError(t, os.MkdirAll(dest, 0755))

	tr := buildTar(t, []tarEntry{
		{name: "evil", typ: tar.TypeSymlink, linkname: outside},
		{name: "evil/pwned", typ: tar.TypeReg, body: "pwned"},
	})
	assert.Error(t, extractTarReader(tr, dest))
	_, err := os.Stat(filepath.Join(outside, "pwned"))
	assert.True(t, os.IsNotExist(err), "file must not be written outside the destination")
}

func TestExtractTarRejectsRelativeSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "feature")
	require.NoError(t, os.MkdirAll(dest, 0755))
	tr := buildTar(t, []tarEntry{
		{name: "sub/", typ: tar.TypeDir},
		{name: "sub/up", typ: tar.TypeSymlink, linkname: "../../"},
	})
	assert.Error(t, extractTarReader(tr, dest))
}

// Even an in-tree symlink must not be used as a directory to write through:
// "a -> ." followed by "a/l -> .." would otherwise escape at resolution time.
func TestExtractTarRejectsWritingThroughSymlink(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "feature")
	require.NoError(t, os.MkdirAll(dest, 0755))
	tr := buildTar(t, []tarEntry{
		{name: "a", typ: tar.TypeSymlink, linkname: "."},
		{name: "a/l", typ: tar.TypeSymlink, linkname: ".."},
	})
	assert.Error(t, extractTarReader(tr, dest))
	_, err := os.Lstat(filepath.Join(dest, "l"))
	assert.True(t, os.IsNotExist(err))
}

func TestExtractTarRejectsPathTraversal(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "feature")
	tr := buildTar(t, []tarEntry{{name: "../escape", typ: tar.TypeReg, body: "x"}})
	assert.Error(t, extractTarReader(tr, dest))
}

func TestExtractTarDropsSetuidBits(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "feature")
	require.NoError(t, os.MkdirAll(dest, 0755))
	tr := buildTar(t, []tarEntry{{name: "bin", typ: tar.TypeReg, body: "x", mode: 04755}})
	require.NoError(t, extractTarReader(tr, dest))
	info, err := os.Stat(filepath.Join(dest, "bin"))
	require.NoError(t, err)
	assert.Zero(t, info.Mode()&os.ModeSetuid)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}
