package features

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFeature(t *testing.T, dir string, digest *DigestInfo) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "devcontainer-feature.json"), []byte(`{"id":"go","version":"1.0.0"}`), 0o644))
	if digest != nil {
		require.NoError(t, saveDigestInfo(dir, *digest))
	}
}

func digestOf(b byte) string { return "sha256:" + strings.Repeat(string("0123456789abcdef"[b%16]), 64) }

// A failed fetch used to leave a partial directory that later runs trusted.
func TestFetchToStoreLeavesNothingOnFailure(t *testing.T) {
	cacheDir := t.TempDir()
	r := &Resolver{cacheDir: cacheDir, report: report.Discard}

	_, err := r.fetchToStore(func(dir string) (*DigestInfo, error) {
		writeFeature(t, dir, nil) // metadata extracted, then the fetch fails
		return nil, errors.New("connection reset")
	}, func(info *DigestInfo) string { return info.Integrity })
	require.Error(t, err)
	entries, _ := os.ReadDir(filepath.Join(cacheDir, storeDir))
	assert.Empty(t, entries, "no store entry and no temp directory may remain")
}

// Store entries never change: fetching a digest that is already stored
// keeps the stored copy (a build may be about to copy it).
func TestFetchToStoreNeverReplacesAnEntry(t *testing.T) {
	cacheDir := t.TempDir()
	r := &Resolver{cacheDir: cacheDir, report: report.Discard}
	digest := digestOf(1)
	fetch := func(marker string) {
		t.Helper()
		_, err := r.fetchToStore(func(dir string) (*DigestInfo, error) {
			writeFeature(t, dir, &DigestInfo{Integrity: digest})
			require.NoError(t, os.WriteFile(filepath.Join(dir, "marker"), []byte(marker), 0o644))
			return &DigestInfo{Integrity: digest}, nil
		}, func(info *DigestInfo) string { return info.Integrity })
		require.NoError(t, err)
	}
	fetch("first")
	fetch("second")
	data, err := os.ReadFile(filepath.Join(r.storePath(digest), "marker"))
	require.NoError(t, err)
	assert.Equal(t, "first", string(data))
	entries, _ := os.ReadDir(filepath.Join(cacheDir, storeDir))
	assert.Len(t, entries, 1, "no temp directory remains")
}

func TestLoadStored(t *testing.T) {
	cacheDir := t.TempDir()
	r := &Resolver{cacheDir: cacheDir, report: report.Discard}
	digest := digestOf(2)

	hit, err := r.loadStored(&Feature{}, digest)
	require.NoError(t, err)
	assert.False(t, hit, "nothing stored")

	writeFeature(t, r.storePath(digest), &DigestInfo{ManifestDigest: digest, Integrity: digestOf(3)})
	f := &Feature{}
	hit, err = r.loadStored(f, digest)
	require.NoError(t, err)
	require.True(t, hit)
	assert.Equal(t, r.storePath(digest), f.CachePath)
	assert.Equal(t, digest, f.ManifestDigest)
	assert.Equal(t, "go", f.Metadata.ID)

	hit, _ = r.loadStored(&Feature{}, "sha256:../../etc")
	assert.False(t, hit, "only real digests name store entries")
	assert.Empty(t, r.storePath("sha256:../../etc"))
}

// A ref records what a tag last resolved to, and when (the TTL).
func TestRefs(t *testing.T) {
	r := &Resolver{cacheDir: t.TempDir(), report: report.Discard}
	_, _, ok := r.readRef("ghcr.io/x/features/y:1")
	assert.False(t, ok)

	r.writeRef("ghcr.io/x/features/y:1", &DigestInfo{ManifestDigest: digestOf(4)})
	info, at, ok := r.readRef("ghcr.io/x/features/y:1")
	require.True(t, ok)
	assert.Equal(t, digestOf(4), info.ManifestDigest)
	assert.WithinDuration(t, time.Now(), at, time.Minute)
}

func TestReadLimited(t *testing.T) {
	data, err := readLimited(strings.NewReader("12345"), 5)
	require.NoError(t, err)
	assert.Equal(t, "12345", string(data))
	_, err = readLimited(strings.NewReader("123456"), 5)
	assert.ErrorContains(t, err, "larger than 5 bytes")
}
