package features

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/griffithind/dcx/internal/lockfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRegistry is an OCI distribution registry serving one feature
// (devcontainers/features/hello) behind Bearer token authentication, like
// ghcr.io: /v2/ answers 401 with a challenge, /token issues the token, and
// manifests and blobs require it.
type fakeRegistry struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	tags      map[string]string // tag -> manifest digest
	manifests map[string][]byte // digest -> manifest
	blobs     map[string][]byte // digest -> blob
	scopes    []string          // scopes token requests asked for
}

const fakeToken = "secret-token"

func newFakeRegistry(t *testing.T) *fakeRegistry {
	t.Helper()
	r := &fakeRegistry{t: t, tags: map[string]string{}, manifests: map[string][]byte{}, blobs: map[string][]byte{}}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)

	// The resolver talks https to the registry host; trust the test
	// server's certificate and point the cache at a temporary directory.
	old := httpClient
	httpClient = r.srv.Client()
	t.Cleanup(func() { httpClient = old })
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return r
}

// host is the registry's host:port, the first part of a feature ID.
func (r *fakeRegistry) host() string { return strings.TrimPrefix(r.srv.URL, "https://") }

func (r *fakeRegistry) featureID(version string) string {
	return r.host() + "/devcontainers/features/hello:" + version
}

// publish stores blob as the feature's layer and points tag at a manifest
// naming layerDigest (the blob's own digest unless overridden). It returns
// the manifest digest.
func (r *fakeRegistry) publish(tag string, blob []byte, layerDigest string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if layerDigest == "" {
		layerDigest = computeIntegrity(blob)
	}
	r.blobs[layerDigest] = blob
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"layers": []map[string]any{{
			"mediaType": "application/vnd.devcontainers.layer.v1+tar",
			"digest":    layerDigest,
			"size":      len(blob),
		}},
	})
	require.NoError(r.t, err)
	digest := computeIntegrity(manifest)
	r.manifests[digest] = manifest
	r.tags[tag] = digest
	return digest
}

func (r *fakeRegistry) serve(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	const repo = "/v2/devcontainers/features/hello/"
	switch {
	case req.URL.Path == "/v2/":
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="fake"`, r.srv.URL))
		w.WriteHeader(http.StatusUnauthorized)
	case req.URL.Path == "/token":
		r.scopes = append(r.scopes, req.URL.Query().Get("scope"))
		_ = json.NewEncoder(w).Encode(map[string]string{"token": fakeToken})
	case req.Header.Get("Authorization") != "Bearer "+fakeToken:
		w.WriteHeader(http.StatusUnauthorized)
	case strings.HasPrefix(req.URL.Path, repo+"manifests/"):
		ref := strings.TrimPrefix(req.URL.Path, repo+"manifests/")
		if digest, ok := r.tags[ref]; ok {
			ref = digest
		}
		manifest, ok := r.manifests[ref]
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Docker-Content-Digest", ref)
		_, _ = w.Write(manifest)
	case strings.HasPrefix(req.URL.Path, repo+"blobs/"):
		blob, ok := r.blobs[strings.TrimPrefix(req.URL.Path, repo+"blobs/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(blob)
	default:
		http.NotFound(w, req)
	}
}

// featureTar builds a feature layer: devcontainer-feature.json at version
// plus any extra entries (name -> content).
func featureTar(t *testing.T, version string, extra map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	write := func(name, content string) {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}))
		_, err := tw.Write([]byte(content))
		require.NoError(t, err)
	}
	write("devcontainer-feature.json", fmt.Sprintf(`{"id": "hello", "version": %q, "dependsOn": {"ghcr.io/devcontainers/features/common-utils:2": {}}}`, version))
	write("install.sh", "#!/bin/sh\necho hello\n")
	for name, content := range extra {
		write(name, content)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

func resolveFeature(t *testing.T, id string, lf *lockfile.Lockfile) (*Feature, error) {
	t.Helper()
	r, err := NewResolver(t.TempDir(), nil)
	require.NoError(t, err)
	return r.ResolveWithLockfile(context.Background(), id, nil, lf)
}

func TestRegistryFetchesFeatureWithToken(t *testing.T) {
	reg := newFakeRegistry(t)
	blob := featureTar(t, "1.0.0", nil)
	manifestDigest := reg.publish("1", blob, "")

	f, err := resolveFeature(t, reg.featureID("1"), nil)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", f.Metadata.Version)
	assert.Equal(t, manifestDigest, f.ManifestDigest)
	assert.Equal(t, computeIntegrity(blob), f.Integrity)
	assert.FileExists(t, filepath.Join(f.CachePath, "install.sh"))
	assert.Equal(t, []string{"repository:devcontainers/features/hello:pull"}, reg.scopes,
		"the token is requested for exactly the feature's repository")

	// A second resolve within the tag TTL is served from the cache.
	_, err = resolveFeature(t, reg.featureID("1"), nil)
	require.NoError(t, err)
	assert.Len(t, reg.scopes, 1, "cached tag: no registry round trip")
}

func TestRegistryRejectsLayerDigestMismatch(t *testing.T) {
	reg := newFakeRegistry(t)
	// The manifest names one blob; the registry (a tampering mirror)
	// serves another under that digest.
	claimed := computeIntegrity(featureTar(t, "1.0.0", nil))
	reg.publish("1", featureTar(t, "6.6.6", nil), claimed)

	_, err := resolveFeature(t, reg.featureID("1"), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "layer digest mismatch")
}

func TestRegistryRejectsLockfileIntegrityMismatch(t *testing.T) {
	reg := newFakeRegistry(t)
	manifestDigest := reg.publish("1", featureTar(t, "1.0.0", nil), "")

	lf := lockfile.New()
	lf.Set(reg.featureID("1"), lockfile.LockedFeature{
		Version:   "1.0.0",
		Resolved:  reg.host() + "/devcontainers/features/hello@" + manifestDigest,
		Integrity: "sha256:" + strings.Repeat("0", 64),
	})
	_, err := resolveFeature(t, reg.featureID("1"), lf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "integrity mismatch")
}

func TestRegistryRejectsPathTraversalInLayer(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.publish("1", featureTar(t, "1.0.0", map[string]string{"../escaped": "x"}), "")

	_, err := resolveFeature(t, reg.featureID("1"), nil)
	require.Error(t, err)
	cache := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "dcx", "features")
	assert.NoFileExists(t, filepath.Join(cache, "escaped"))
	entries, _ := os.ReadDir(cache)
	for _, e := range entries {
		assert.True(t, strings.HasPrefix(e.Name(), "."), "a failed fetch leaves no cache entry, found %s", e.Name())
	}
}

func TestRegistryLockfilePinSurvivesAMovedTag(t *testing.T) {
	reg := newFakeRegistry(t)
	reg.publish("1", featureTar(t, "1.0.0", nil), "")

	first, err := resolveFeature(t, reg.featureID("1"), nil)
	require.NoError(t, err)
	lf := GenerateLockfile([]*Feature{first})
	locked, ok := lf.Get(reg.featureID("1"))
	require.True(t, ok)
	assert.Equal(t, lockedFeatureFor(reg, first), locked)
	assert.Empty(t, VerifyLockfile([]*Feature{first}, lf))

	// The publisher moves tag 1 to a new release.
	reg.publish("1", featureTar(t, "1.1.0", nil), "")

	// A fresh cache: the pin fetches the locked manifest by digest...
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	pinned, err := resolveFeature(t, reg.featureID("1"), lf)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", pinned.Metadata.Version)
	assert.Empty(t, VerifyLockfile([]*Feature{pinned}, lf))

	// ...while an unpinned resolve follows the tag, and verification
	// reports how it differs from the lockfile.
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	moved, err := resolveFeature(t, reg.featureID("1"), nil)
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", moved.Metadata.Version)
	var types []MismatchType
	for _, m := range VerifyLockfile([]*Feature{moved}, lf) {
		types = append(types, m.Type)
	}
	assert.ElementsMatch(t, []MismatchType{MismatchVersion, MismatchIntegrity}, types)
}

// lockedFeatureFor is the lockfile entry a feature fetched from reg must get.
func lockedFeatureFor(reg *fakeRegistry, f *Feature) lockfile.LockedFeature {
	return lockfile.LockedFeature{
		Version:   f.Metadata.Version,
		Resolved:  reg.host() + "/devcontainers/features/hello@" + f.ManifestDigest,
		Integrity: f.Integrity,
		DependsOn: []string{"ghcr.io/devcontainers/features/common-utils:2"},
	}
}

func TestVerifyLockfileReportsMissingAndExtra(t *testing.T) {
	lf := lockfile.New()
	lf.Set("ghcr.io/devcontainers/features/go:1", lockfile.LockedFeature{Integrity: "sha256:a"})
	features := []*Feature{
		{ID: "ghcr.io/devcontainers/features/node:1", Source: FeatureSource{Type: SourceTypeOCI}},
		{ID: "./local", Source: FeatureSource{Type: SourceTypeLocalPath}},
	}
	mismatches := VerifyLockfile(features, lf)
	require.Len(t, mismatches, 2, "local features are never locked")
	byType := map[MismatchType]string{}
	for _, m := range mismatches {
		byType[m.Type] = m.FeatureID
	}
	assert.Equal(t, "ghcr.io/devcontainers/features/node:1", byType[MismatchMissing])
	assert.Equal(t, "ghcr.io/devcontainers/features/go:1", byType[MismatchExtra])

	assert.Empty(t, GenerateLockfile(features).Features, "no integrity, local: nothing to lock")
}
