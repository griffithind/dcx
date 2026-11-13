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

// A failed fetch used to leave a partial directory that later runs trusted.
func TestFetchIntoLeavesNothingOnFailure(t *testing.T) {
	cacheDir := t.TempDir()
	r := &Resolver{cacheDir: cacheDir, report: report.Discard}
	cachePath := filepath.Join(cacheDir, "feat")

	err := r.fetchInto(cachePath, func(dir string) error {
		writeFeature(t, dir, nil) // metadata extracted, then the fetch fails
		return errors.New("connection reset")
	})
	require.Error(t, err)
	entries, _ := os.ReadDir(cacheDir)
	assert.Empty(t, entries, "no cache entry and no temp directory may remain")
}

func TestFetchIntoInstallsCompleteEntry(t *testing.T) {
	cacheDir := t.TempDir()
	r := &Resolver{cacheDir: cacheDir, report: report.Discard}
	cachePath := filepath.Join(cacheDir, "feat")
	writeFeature(t, cachePath, &DigestInfo{Integrity: "sha256:old"}) // stale entry is replaced

	require.NoError(t, r.fetchInto(cachePath, func(dir string) error {
		writeFeature(t, dir, &DigestInfo{Integrity: "sha256:new"})
		return nil
	}))
	info, err := loadDigestInfo(cachePath)
	require.NoError(t, err)
	assert.Equal(t, "sha256:new", info.Integrity)
	entries, _ := os.ReadDir(cacheDir)
	assert.Len(t, entries, 1)
}

func TestUseCache(t *testing.T) {
	cacheDir := t.TempDir()
	r := &Resolver{cacheDir: cacheDir, report: report.Discard}

	t.Run("incomplete entry is removed", func(t *testing.T) {
		path := filepath.Join(cacheDir, "incomplete")
		writeFeature(t, path, nil)
		hit, err := r.useCache(&Feature{}, path, "", "f", 0)
		require.NoError(t, err)
		assert.False(t, hit)
		assert.NoDirExists(t, path)
	})
	t.Run("complete entry is used", func(t *testing.T) {
		path := filepath.Join(cacheDir, "complete")
		writeFeature(t, path, &DigestInfo{ManifestDigest: "sha256:m", Integrity: "sha256:i"})
		f := &Feature{}
		hit, err := r.useCache(f, path, "sha256:i", "f", 0)
		require.NoError(t, err)
		assert.True(t, hit)
		assert.Equal(t, path, f.CachePath)
		assert.Equal(t, "sha256:m", f.ManifestDigest)
		assert.Equal(t, "go", f.Metadata.ID)
	})
	t.Run("integrity mismatch is removed", func(t *testing.T) {
		path := filepath.Join(cacheDir, "mismatch")
		writeFeature(t, path, &DigestInfo{Integrity: "sha256:i"})
		hit, err := r.useCache(&Feature{}, path, "sha256:other", "f", 0)
		require.NoError(t, err)
		assert.False(t, hit)
		assert.NoDirExists(t, path)
	})
	t.Run("an expired tag entry is re-checked but kept", func(t *testing.T) {
		path := filepath.Join(cacheDir, "expired")
		writeFeature(t, path, &DigestInfo{Integrity: "sha256:i"})
		old := time.Now().Add(-2 * ociTagTTL)
		require.NoError(t, os.Chtimes(filepath.Join(path, digestFileName), old, old))
		hit, err := r.useCache(&Feature{}, path, "", "f", ociTagTTL)
		require.NoError(t, err)
		assert.False(t, hit, "a moving tag is checked again after the TTL")
		assert.DirExists(t, path, "kept as the fallback when the registry is unreachable")
		hit, err = r.useCache(&Feature{}, path, "", "f", 0)
		require.NoError(t, err)
		assert.True(t, hit)
	})
	t.Run("force pull ignores the cache", func(t *testing.T) {
		path := filepath.Join(cacheDir, "forced")
		writeFeature(t, path, &DigestInfo{Integrity: "sha256:i"})
		forced := &Resolver{cacheDir: cacheDir, report: report.Discard, forcePull: true}
		hit, err := forced.useCache(&Feature{}, path, "", "f", 0)
		require.NoError(t, err)
		assert.False(t, hit)
	})
}

func TestReadLimited(t *testing.T) {
	data, err := readLimited(strings.NewReader("12345"), 5)
	require.NoError(t, err)
	assert.Equal(t, "12345", string(data))
	_, err = readLimited(strings.NewReader("123456"), 5)
	assert.ErrorContains(t, err, "larger than 5 bytes")
}
