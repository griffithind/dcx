package container

import (
	"archive/tar"
	"bytes"
	"io"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The archive WriteFiles sends is root-owned whatever the host user, with
// the given modes, relative to / and in order.
func TestFilesArchive(t *testing.T) {
	data, err := filesArchive([]File{
		{Path: "/var/lib/dcx/lifecycle", Mode: fs.ModeDir | 0o755},
		{Path: "/var/lib/dcx/lifecycle/ready.done", Mode: 0o600, Data: []byte("stamp\n")},
	})
	require.NoError(t, err)
	tr := tar.NewReader(bytes.NewReader(data))
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		names = append(names, hdr.Name)
		assert.Zero(t, hdr.Uid)
		assert.Zero(t, hdr.Gid)
		if hdr.Typeflag == tar.TypeDir {
			assert.EqualValues(t, 0o755, hdr.Mode)
		} else {
			assert.EqualValues(t, 0o600, hdr.Mode)
			body, _ := io.ReadAll(tr)
			assert.Equal(t, "stamp\n", string(body))
		}
	}
	assert.Equal(t, []string{"var/lib/dcx/lifecycle/", "var/lib/dcx/lifecycle/ready.done"}, names)

	_, err = filesArchive([]File{{Path: "relative", Mode: 0o600}})
	assert.Error(t, err)
}

// dirFromArchive keeps the regular files directly in the directory
// `docker cp <c>:<dir> -` archived.
func TestDirFromArchive(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	add := func(name string, typ byte, body string) {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Typeflag: typ, Mode: 0o644, Size: int64(len(body))}))
		_, err := tw.Write([]byte(body))
		require.NoError(t, err)
	}
	add("lifecycle/", tar.TypeDir, "")
	add("lifecycle/ready.done", tar.TypeReg, "t1\n")
	add("lifecycle/sub/", tar.TypeDir, "")
	add("lifecycle/sub/deeper.done", tar.TypeReg, "x")
	require.NoError(t, tw.Close())

	files, err := dirFromArchive(&buf)
	require.NoError(t, err)
	assert.Equal(t, map[string][]byte{"ready.done": []byte("t1\n")}, files)
}
