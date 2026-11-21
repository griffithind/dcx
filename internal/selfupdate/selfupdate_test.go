package selfupdate

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersions(t *testing.T) {
	assert.True(t, IsRelease("v0.8.0"))
	for _, v := range []string{"dev", "0.8.0", "v0.8", "v0.7.0-53-gf398798-dirty", "v0.9.0-rc.1", "abc1234"} {
		assert.False(t, IsRelease(v), v)
	}
	assert.True(t, Newer("v0.8.1", "v0.8.0"))
	assert.True(t, Newer("v0.10.0", "v0.9.9"), "numeric, not lexical")
	assert.True(t, Newer("v1.0.0", "v0.99.99"))
	assert.False(t, Newer("v0.8.0", "v0.8.0"))
	assert.False(t, Newer("v0.7.9", "v0.8.0"), "never downgrades")
	assert.False(t, Newer("v0.9.0", "dev"))
}

// fakeRelease serves a latest release tag and its assets the way GitHub
// does, with the binary behind a redirect.
type fakeRelease struct {
	tag       string
	binary    []byte
	checksums string // default: the right one
}

func (f *fakeRelease) serve(t *testing.T) *Updater {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/griffithind/dcx/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprintf(w, `{"tag_name": %q, "assets": []}`, f.tag)
	})
	mux.HandleFunc("/griffithind/dcx/releases/download/"+f.tag+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		if f.checksums != "" {
			_, _ = w.Write([]byte(f.checksums))
			return
		}
		_, _ = fmt.Fprintf(w, "%x  dcx-darwin-arm64\n%x  dcx-linux-amd64\n", sha256.Sum256([]byte("other")), sha256.Sum256(f.binary))
	})
	mux.HandleFunc("/griffithind/dcx/releases/download/"+f.tag+"/dcx-linux-amd64", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/storage/dcx-linux-amd64", http.StatusFound)
	})
	mux.HandleFunc("/storage/dcx-linux-amd64", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(f.binary) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u := New()
	u.API, u.Downloads, u.GOOS, u.GOARCH = srv.URL, srv.URL, "linux", "amd64"
	return u
}

func TestLatestAndDownload(t *testing.T) {
	f := &fakeRelease{tag: "v0.9.0", binary: []byte("new dcx")}
	u := f.serve(t)
	tag, err := u.Latest(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v0.9.0", tag)
	binary, err := u.Download(context.Background(), tag)
	require.NoError(t, err)
	assert.Equal(t, "new dcx", string(binary))
}

func TestDownloadRefusesUnverifiedBinaries(t *testing.T) {
	ctx := context.Background()
	f := &fakeRelease{tag: "v0.9.0", binary: []byte("tampered"),
		checksums: fmt.Sprintf("%x  dcx-linux-amd64\n", sha256.Sum256([]byte("original")))}
	_, err := f.serve(t).Download(ctx, "v0.9.0")
	assert.ErrorContains(t, err, "does not match checksums.txt")

	f = &fakeRelease{tag: "v0.9.0", binary: []byte("x"), checksums: fmt.Sprintf("%x  dcx-darwin-arm64\n", sha256.Sum256([]byte("x")))}
	_, err = f.serve(t).Download(ctx, "v0.9.0")
	assert.ErrorContains(t, err, "no entry for dcx-linux-amd64")

	_, err = f.serve(t).Download(ctx, "../../evil")
	assert.ErrorContains(t, err, "not a release version")
}

func TestLatestRejectsNonVersionTags(t *testing.T) {
	_, err := (&fakeRelease{tag: "nightly"}).serve(t).Latest(context.Background())
	assert.ErrorContains(t, err, "not a version")
}

func TestRedirectToPlainHTTPIsRefused(t *testing.T) {
	err := New().Client.CheckRedirect(
		&http.Request{URL: mustURL(t, "http://storage.example/dcx")},
		[]*http.Request{{URL: mustURL(t, "https://github.com/x")}})
	assert.ErrorContains(t, err, "refusing redirect")
}

// fakeDCX is a script standing in for a dcx binary that reports version.
func fakeDCX(version string) []byte {
	return []byte("#!/bin/sh\necho 'dcx version " + version + "'\n")
}

func TestInstallReplacesTheBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	path := filepath.Join(t.TempDir(), "dcx")
	require.NoError(t, os.WriteFile(path, fakeDCX("v0.8.0"), 0o755))
	require.NoError(t, Install(context.Background(), path, fakeDCX("v0.9.0"), "v0.9.0"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, fakeDCX("v0.9.0"), data)
	st, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o755), st.Mode().Perm())
	entries, _ := os.ReadDir(filepath.Dir(path))
	assert.Len(t, entries, 1, "no temporary file left behind")
}

// A binary that does not run or reports another version is not installed,
// and the old one stays.
func TestInstallKeepsTheOldBinaryOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script stand-in")
	}
	for name, binary := range map[string][]byte{
		"wrong version": fakeDCX("v0.8.5"),
		"does not run":  []byte("\x7fELF garbage"),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "dcx")
			require.NoError(t, os.WriteFile(path, fakeDCX("v0.8.0"), 0o755))
			err := Install(context.Background(), path, binary, "v0.9.0")
			assert.ErrorContains(t, err, "not installing it")
			data, _ := os.ReadFile(path)
			assert.Equal(t, fakeDCX("v0.8.0"), data)
			entries, _ := os.ReadDir(filepath.Dir(path))
			assert.Len(t, entries, 1, "no temporary file left behind")
		})
	}
}

func TestInstallInReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "dcx")
	require.NoError(t, os.WriteFile(path, fakeDCX("v0.8.0"), 0o755))
	require.NoError(t, os.Chmod(dir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	err := Install(context.Background(), path, fakeDCX("v0.9.0"), "v0.9.0")
	assert.ErrorContains(t, err, "is not writable by you")
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	require.NoError(t, err)
	return u
}
