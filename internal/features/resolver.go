package features

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/lockfile"
	"github.com/griffithind/dcx/internal/registryauth"
	"github.com/griffithind/dcx/internal/report"
	"github.com/tidwall/jsonc"
)

// httpClient is the HTTP client with timeout for registry requests.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// Resolver handles feature resolution and caching.
type Resolver struct {
	cacheDir  string
	configDir string
	forcePull bool
	report    report.Reporter
}

// DigestInfo holds digest information for a resolved feature.
type DigestInfo struct {
	ManifestDigest string `json:"manifest_digest,omitempty"` // OCI manifest digest
	Integrity      string `json:"integrity"`                 // Tarball SHA256 hash
}

const digestFileName = ".dcx-integrity"

// ociTagTTL is how long a feature fetched by tag is used before the
// registry is asked again whether the tag moved (D9).
const ociTagTTL = 24 * time.Hour

// computeIntegrity computes the SHA256 integrity hash of data.
// Returns format: "sha256:hexstring"
func computeIntegrity(data []byte) string {
	hash := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(hash[:])
}

// saveDigestInfo saves digest information to the cache directory.
func saveDigestInfo(cachePath string, info DigestInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cachePath, digestFileName), data, 0644)
}

// loadDigestInfo loads digest information from the cache directory.
func loadDigestInfo(cachePath string) (*DigestInfo, error) {
	data, err := os.ReadFile(filepath.Join(cachePath, digestFileName))
	if err != nil {
		return nil, err
	}
	var info DigestInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// verifyIntegrity verifies that data matches the expected integrity hash.
func verifyIntegrity(data []byte, expected string) error {
	if expected == "" {
		return nil // No expected integrity, skip verification
	}
	actual := computeIntegrity(data)
	if actual != expected {
		return fmt.Errorf("integrity mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

// NewResolver creates a new feature resolver reporting fetch progress to rep
// (nil discards it).
func NewResolver(configDir string, rep report.Reporter) (*Resolver, error) {
	// Determine cache directory
	cacheDir, err := getCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to determine cache directory: %w", err)
	}

	return &Resolver{
		cacheDir:  cacheDir,
		configDir: configDir,
		report:    report.OrDiscard(rep),
	}, nil
}

// SetForcePull configures the resolver to force re-fetch features from the registry.
func (r *Resolver) SetForcePull(forcePull bool) {
	r.forcePull = forcePull
}

// getCacheDir returns the feature cache directory.
func getCacheDir() (string, error) {
	// Use XDG_CACHE_HOME if set, otherwise ~/.cache
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		cacheHome = filepath.Join(home, ".cache")
	}

	return filepath.Join(cacheHome, "dcx", "features"), nil
}

// ResolveWithLockfile resolves a feature, optionally using a lockfile for pinned versions.
func (r *Resolver) ResolveWithLockfile(ctx context.Context, id string, options map[string]interface{}, lockfile *lockfile.Lockfile) (*Feature, error) {
	// Parse the feature reference
	ref, err := ParseFeatureSource(id)
	if err != nil {
		return nil, fmt.Errorf("failed to parse feature ID %q: %w", id, err)
	}

	feature := &Feature{
		ID:      id,
		Source:  ref,
		Options: options,
	}

	// Resolve based on reference type
	switch ref.Type {
	case SourceTypeLocalPath:
		if err := r.resolveLocal(ctx, feature); err != nil {
			return nil, fmt.Errorf("failed to resolve local feature: %w", err)
		}
	case SourceTypeOCI:
		if err := r.resolveOCIWithLockfile(ctx, feature, lockfile); err != nil {
			return nil, fmt.Errorf("failed to resolve OCI feature: %w", err)
		}
	case SourceTypeTarball:
		if err := r.resolveHTTPWithLockfile(ctx, feature, lockfile); err != nil {
			return nil, fmt.Errorf("failed to resolve HTTP feature: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported feature reference type: %s", ref.Type)
	}

	return feature, nil
}

// resolveLocal resolves a local feature. Its folder must be inside the
// config's .devcontainer folder (or the config file's folder), as
// devcontainers/cli requires.
func (r *Resolver) resolveLocal(ctx context.Context, feature *Feature) error {
	path := filepath.Join(r.configDir, feature.Source.Path)
	if root := localFeatureRoot(r.configDir); !isWithinDir(root, path) {
		return fmt.Errorf("local feature %s must be inside %s", feature.Source.Path, root)
	}

	// Verify directory exists
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("feature directory not found: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("feature path is not a directory: %s", path)
	}

	feature.CachePath = path

	// Load metadata
	metadata, err := r.loadMetadata(path)
	if err != nil {
		return fmt.Errorf("failed to load feature metadata: %w", err)
	}
	feature.Metadata = metadata

	return nil
}

// localFeatureRoot is the folder local features must be in: the nearest
// .devcontainer folder containing the config file, else the config file's
// own folder.
func localFeatureRoot(configDir string) string {
	for dir := filepath.Clean(configDir); ; dir = filepath.Dir(dir) {
		if filepath.Base(dir) == ".devcontainer" {
			return dir
		}
		if filepath.Dir(dir) == dir {
			return filepath.Clean(configDir)
		}
	}
}

// The feature cache ($XDG_CACHE_HOME/dcx/features) has two parts:
//
//	sha256/<hex>/  feature contents, by OCI manifest digest or tarball
//	               SHA-256; written once (atomically) and never replaced,
//	               so a resolved feature's CachePath keeps its bytes until
//	               the build has copied them, whatever another resolution
//	               (another pin, a moved tag) fetches meanwhile;
//	refs/<key>     the digest a tag or URL last resolved to (its mtime is
//	               when), so a tag is re-checked after ociTagTTL.
const (
	storeDir = "sha256"
	refsDir  = "refs"
)

// resolveOCIWithLockfile resolves an OCI feature. A lockfile entry pins it
// to the manifest digest it records (its integrity, as devcontainers/cli
// writes it); so does a reference by digest.
func (r *Resolver) resolveOCIWithLockfile(ctx context.Context, feature *Feature, lf *lockfile.Lockfile) error {
	ref := feature.Source
	name := ref.CanonicalID()

	var lockedManifestDigest string
	if locked, ok := lf.Get(feature.ID); ok && strings.HasPrefix(locked.Integrity, "sha256:") {
		lockedManifestDigest = locked.Integrity
	}
	pin := lockedManifestDigest
	if pin == "" && ref.IsDigest() {
		pin = ref.Version
	}

	// A digest never moves: a stored copy is it. A tag (":1", ":latest")
	// moves: it is re-checked after ociTagTTL.
	if !r.forcePull {
		if pin != "" {
			if hit, err := r.loadStored(feature, pin); hit || err != nil {
				return err
			}
		} else if info, fetched, ok := r.readRef(name); ok && time.Since(fetched) < ociTagTTL {
			if hit, err := r.loadStored(feature, info.ManifestDigest); hit || err != nil {
				return err
			}
		} else if ok {
			r.report.Infof("  Checking %s for updates...", name)
		}
	}

	if lockedManifestDigest != "" {
		r.report.Infof("  Fetching feature from registry: %s (locked to %s)", name, lockedManifestDigest[:min(19, len(lockedManifestDigest))]+"...")
	} else {
		r.report.Infof("  Fetching feature from registry: %s", name)
	}
	info, err := r.fetchToStore(func(dir string) (*DigestInfo, error) {
		return r.fetchOCIWithDigest(ctx, ref, dir, pin)
	}, func(info *DigestInfo) string { return info.ManifestDigest })
	if err != nil {
		// A tag falls back to the copy it last resolved to.
		if pin == "" {
			if last, _, ok := r.readRef(name); ok {
				if hit, _ := r.loadStored(feature, last.ManifestDigest); hit {
					r.report.Warnf("Could not refresh %s (%v); using the cached copy", name, err)
					return nil
				}
			}
		}
		if lockedManifestDigest != "" {
			return fmt.Errorf("failed to fetch OCI feature %s at the digest devcontainer-lock.json pins (a lockfile written by an older dcx records another digest: run 'dcx lock'): %w", feature.ID, err)
		}
		return fmt.Errorf("failed to fetch OCI feature: %w", err)
	}
	if pin == "" {
		r.writeRef(name, info)
	}
	if hit, err := r.loadStored(feature, info.ManifestDigest); !hit || err != nil {
		return fmt.Errorf("feature %s: stored copy missing after fetch: %v", name, err)
	}
	return nil
}

// resolveHTTPWithLockfile resolves an HTTP feature, optionally using lockfile for integrity verification.
func (r *Resolver) resolveHTTPWithLockfile(ctx context.Context, feature *Feature, lockfile *lockfile.Lockfile) error {
	url := feature.Source.URL

	var expectedIntegrity string
	if locked, ok := lockfile.Get(feature.ID); ok {
		expectedIntegrity = locked.Integrity
	}

	// A tarball URL is fetched once; the lockfile pins its SHA-256.
	if !r.forcePull {
		digest := expectedIntegrity
		if digest == "" {
			if info, _, ok := r.readRef(url); ok {
				digest = info.Integrity
			}
		}
		if digest != "" {
			if hit, err := r.loadStored(feature, digest); hit || err != nil {
				return err
			}
		}
	}

	info, err := r.fetchToStore(func(dir string) (*DigestInfo, error) {
		integrity, err := r.fetchHTTPWithDigest(ctx, url, dir, expectedIntegrity)
		if err != nil {
			return nil, err
		}
		return &DigestInfo{Integrity: integrity}, nil
	}, func(info *DigestInfo) string { return info.Integrity })
	if err != nil {
		return fmt.Errorf("failed to fetch HTTP feature: %w", err)
	}
	if expectedIntegrity == "" {
		r.writeRef(url, info)
	}
	if hit, err := r.loadStored(feature, info.Integrity); !hit || err != nil {
		return fmt.Errorf("feature %s: stored copy missing after fetch: %v", url, err)
	}
	return nil
}

// storePath is the store directory of digest ("sha256:<hex>"), or "" for
// anything else.
func (r *Resolver) storePath(digest string) string {
	hexPart, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || len(hexPart) != 64 || strings.Trim(hexPart, "0123456789abcdef") != "" {
		return ""
	}
	return filepath.Join(r.cacheDir, storeDir, hexPart)
}

// loadStored fills feature from the stored copy of digest, reporting
// whether there is one.
func (r *Resolver) loadStored(feature *Feature, digest string) (bool, error) {
	path := r.storePath(digest)
	if path == "" {
		return false, nil
	}
	info, err := loadDigestInfo(path)
	if err != nil {
		return false, nil
	}
	metadata, err := r.loadMetadata(path)
	if err != nil {
		return false, fmt.Errorf("failed to load cached feature metadata: %w", err)
	}
	feature.CachePath = path
	feature.Metadata = metadata
	feature.ManifestDigest = info.ManifestDigest
	feature.Integrity = info.Integrity
	return true, nil
}

// fetchToStore runs fetch into a fresh temporary directory and moves the
// result into the store under key(info). A copy already stored (another
// dcx run fetched it, or --pull fetched it again) is kept as it is: store
// entries never change. Failures leave nothing behind.
func (r *Resolver) fetchToStore(fetch func(dir string) (*DigestInfo, error), key func(*DigestInfo) string) (*DigestInfo, error) {
	store := filepath.Join(r.cacheDir, storeDir)
	if err := os.MkdirAll(store, 0o755); err != nil {
		return nil, fmt.Errorf("create feature cache: %w", err)
	}
	tmp, err := os.MkdirTemp(store, ".fetch-")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	info, err := fetch(tmp)
	if err != nil {
		return nil, err
	}
	dest := r.storePath(key(info))
	if dest == "" {
		return nil, fmt.Errorf("feature digest %q is not sha256", key(info))
	}
	if _, err := loadDigestInfo(dest); err == nil {
		return info, nil
	}
	if err := os.Rename(tmp, dest); err != nil {
		// Another dcx run may have stored the same digest first.
		if _, derr := loadDigestInfo(dest); derr == nil {
			return info, nil
		}
		return nil, fmt.Errorf("install feature cache: %w", err)
	}
	return info, nil
}

// readRef returns the digest name (a tag reference or a URL) last resolved
// to, and when.
func (r *Resolver) readRef(name string) (DigestInfo, time.Time, bool) {
	path := filepath.Join(r.cacheDir, refsDir, computeCacheKey(name))
	st, err := os.Stat(path)
	if err != nil {
		return DigestInfo{}, time.Time{}, false
	}
	data, err := os.ReadFile(path)
	var info DigestInfo
	if err != nil || json.Unmarshal(data, &info) != nil {
		return DigestInfo{}, time.Time{}, false
	}
	return info, st.ModTime(), true
}

// writeRef records what name resolved to (atomically; a failure only costs
// a fetch next time).
func (r *Resolver) writeRef(name string, info *DigestInfo) {
	dir := filepath.Join(r.cacheDir, refsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	data, err := json.Marshal(info)
	if err != nil {
		return
	}
	f, err := os.CreateTemp(dir, ".ref-")
	if err != nil {
		return
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil || os.Rename(f.Name(), filepath.Join(dir, computeCacheKey(name))) != nil {
		_ = os.Remove(f.Name())
	}
}

// Limits on what a registry or URL may send. Features are small scripts;
// these only stop a hostile or broken server from exhausting memory.
const (
	maxManifestSize = 4 << 20   // 4 MiB
	maxBlobSize     = 512 << 20 // 512 MiB
	maxErrorBody    = 64 << 10  // 64 KiB

	// maxExtractedSize bounds the total size a feature archive may expand to.
	maxExtractedSize = 1 << 30 // 1 GiB
)

// readLimited reads at most limit bytes and fails if there is more.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response larger than %d bytes", limit)
	}
	return data, nil
}

// fetchOCIWithDigest fetches a feature from an OCI registry and returns digest info.
// If lockedManifestDigest is provided (from lockfile), it fetches the manifest by digest
// instead of by tag, ensuring exact reproducibility. A manifest fetched by
// digest (pinned by the lockfile or the reference) must hash to it.
func (r *Resolver) fetchOCIWithDigest(ctx context.Context, ref FeatureSource, destPath string, lockedManifestDigest string) (*DigestInfo, error) {
	manifestReference := ref.Version
	if lockedManifestDigest != "" {
		manifestReference = lockedManifestDigest
	}
	base := fmt.Sprintf("%s://%s/v2/%s/%s", ref.scheme(), ref.Registry, ref.Repository, ref.Resource)
	manifestURL := base + "/manifests/" + manifestReference

	// Get token for authentication (required for most OCI registries)
	token, tokenErr := r.getRegistryToken(ctx, ref)
	if tokenErr != nil {
		// Continue without a token (some registries allow anonymous
		// pulls); the error is reported if the registry then refuses.
		token = ""
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", manifestURL, nil)
	if err != nil {
		return nil, err
	}

	// Accept OCI manifest types
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	if token != "" {
		req.Header.Set("Authorization", authorization(token))
	}

	// Make request
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Close error irrelevant after read

	if resp.StatusCode != http.StatusOK {
		body, _ := readLimited(resp.Body, maxErrorBody)
		if tokenErr != nil && (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
			return nil, fmt.Errorf("registry returned %d (authentication failed: %v): %s", resp.StatusCode, tokenErr, string(body))
		}
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	manifestBody, err := readLimited(resp.Body, maxManifestSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	// The manifest digest is the hash of the bytes received, never a
	// header the registry (or a proxy) sends.
	manifestDigest := computeIntegrity(manifestBody)
	if strings.HasPrefix(manifestReference, "sha256:") && manifestDigest != manifestReference {
		return nil, fmt.Errorf("feature %s/%s/%s: manifest digest mismatch: asked for %s, got %s",
			ref.Registry, ref.Repository, ref.Resource, manifestReference, manifestDigest)
	}

	// Parse manifest
	var manifest struct {
		Layers []struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
			Size      int64  `json:"size"`
		} `json:"layers"`
	}

	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("no layers found in manifest")
	}

	// Find the feature layer (usually the first tar.gz layer)
	var featureLayer struct {
		Digest    string
		MediaType string
	}
	for _, layer := range manifest.Layers {
		if strings.Contains(layer.MediaType, "tar") {
			featureLayer.Digest = layer.Digest
			featureLayer.MediaType = layer.MediaType
			break
		}
	}

	if featureLayer.Digest == "" {
		return nil, fmt.Errorf("no feature layer found in manifest")
	}

	// Fetch the layer blob
	blobURL := base + "/blobs/" + featureLayer.Digest

	blobReq, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		blobReq.Header.Set("Authorization", authorization(token))
	}

	blobResp, err := httpClient.Do(blobReq)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch blob: %w", err)
	}
	defer blobResp.Body.Close() //nolint:errcheck // Close error irrelevant after read

	if blobResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch blob: status %d", blobResp.StatusCode)
	}

	// Read entire body first (needed for digest computation and extraction)
	bodyData, err := readLimited(blobResp.Body, maxBlobSize)
	if err != nil {
		return nil, fmt.Errorf("failed to read blob body: %w", err)
	}

	// Compute tarball integrity
	integrity := computeIntegrity(bodyData)

	// The blob is content-addressed: it must hash to the digest the manifest
	// names, otherwise a tampering mirror or proxy could substitute it.
	if strings.HasPrefix(featureLayer.Digest, "sha256:") && integrity != featureLayer.Digest {
		return nil, fmt.Errorf("feature %s/%s/%s: layer digest mismatch: manifest says %s, got %s",
			ref.Registry, ref.Repository, ref.Resource, featureLayer.Digest, integrity)
	}

	// Extract the tarball based on media type
	if strings.Contains(featureLayer.MediaType, "gzip") {
		if err := extractTarGz(bytes.NewReader(bodyData), destPath); err != nil {
			return nil, fmt.Errorf("failed to extract gzip feature: %w", err)
		}
	} else {
		// Assume uncompressed tar
		if err := extractTar(bytes.NewReader(bodyData), destPath); err != nil {
			return nil, fmt.Errorf("failed to extract feature: %w", err)
		}
	}

	// Save digest info to cache
	digestInfo := &DigestInfo{
		ManifestDigest: manifestDigest,
		Integrity:      integrity,
	}
	// The digest file marks a store entry complete (loadStored).
	if err := saveDigestInfo(destPath, *digestInfo); err != nil {
		return nil, fmt.Errorf("save digest info: %w", err)
	}

	return digestInfo, nil
}

// fetchHTTPWithDigest fetches a feature from an HTTP URL and returns integrity hash.
func (r *Resolver) fetchHTTPWithDigest(ctx context.Context, url, destPath string, expectedIntegrity string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Close error irrelevant after read

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP request failed with status %d", resp.StatusCode)
	}

	// Read entire body for integrity computation
	bodyData, err := readLimited(resp.Body, maxBlobSize)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Compute integrity
	integrity := computeIntegrity(bodyData)

	// Verify integrity against expected if provided
	if err := verifyIntegrity(bodyData, expectedIntegrity); err != nil {
		return "", fmt.Errorf("feature %s: %w", url, err)
	}

	// Extract the tarball
	if err := extractTarGz(bytes.NewReader(bodyData), destPath); err != nil {
		return "", fmt.Errorf("failed to extract feature: %w", err)
	}

	// Save digest info to cache
	digestInfo := DigestInfo{
		Integrity: integrity,
	}
	// The digest file marks a store entry complete (loadStored).
	if err := saveDigestInfo(destPath, digestInfo); err != nil {
		return "", fmt.Errorf("save digest info: %w", err)
	}

	return integrity, nil
}

// loadMetadata loads the devcontainer-feature.json from a feature directory.
func (r *Resolver) loadMetadata(path string) (*FeatureMetadata, error) {
	metadataPath := filepath.Join(path, "devcontainer-feature.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read devcontainer-feature.json: %w", err)
	}

	// JSONC, like devcontainer.json (comments, trailing commas, a BOM).
	data = jsonc.ToJSON(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF}))
	var metadata FeatureMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to parse devcontainer-feature.json: %w", err)
	}

	return &metadata, nil
}

// computeCacheKey computes a cache key from an identifier.
func computeCacheKey(id string) string {
	hash := sha256.Sum256([]byte(id))
	return hex.EncodeToString(hash[:])[:16]
}

// extractTar extracts an uncompressed tar archive to a directory.
func extractTar(r io.Reader, destPath string) error {
	// Create destination directory
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return extractTarReader(tar.NewReader(r), destPath)
}

// extractTarGz extracts a tar.gz archive to a directory.
func extractTarGz(r io.Reader, destPath string) error {
	// Create destination directory
	if err := os.MkdirAll(destPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create gzip reader
	gzr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzr.Close() //nolint:errcheck // Close error irrelevant after read

	return extractTarReader(tar.NewReader(gzr), destPath)
}

// extractTarReader extracts a tar reader to a directory.
//
// Feature archives come from remote registries, so every entry must stay
// inside destPath: names are cleaned and prefix-checked, entries are never
// written through a symlink, and symlink targets must resolve inside
// destPath. Setuid/setgid/sticky bits are dropped.
func extractTarReader(tr *tar.Reader, destPath string) error {
	cleanDestPath := filepath.Clean(destPath)
	if err := os.MkdirAll(cleanDestPath, 0755); err != nil {
		return fmt.Errorf("failed to create destination: %w", err)
	}
	fileCount := 0

	var total int64
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar: %w", err)
		}

		// Sanitize path to prevent path traversal
		cleanName := filepath.Clean(header.Name)
		// Skip root directory entry
		if cleanName == "." {
			continue
		}
		target := filepath.Join(cleanDestPath, cleanName)
		if !isWithinDir(cleanDestPath, target) {
			return fmt.Errorf("invalid tar path: %s", header.Name)
		}
		if err := ensureNoSymlinkComponents(cleanDestPath, target); err != nil {
			return fmt.Errorf("invalid tar path %s: %w", header.Name, err)
		}

		fileCount++

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0755); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			// Create file (refusing to follow a pre-existing symlink)
			if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("invalid tar path %s: refusing to write through symlink", header.Name)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode).Perm())
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			// Bound the expanded size: a small compressed blob can
			// expand to fill the disk.
			n, err := io.Copy(f, io.LimitReader(tr, maxExtractedSize-total+1))
			total += n
			if err == nil && total > maxExtractedSize {
				err = fmt.Errorf("archive expands beyond %d bytes", maxExtractedSize)
			}
			if err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			_ = f.Close()
		case tar.TypeSymlink:
			if filepath.IsAbs(header.Linkname) {
				return fmt.Errorf("invalid symlink %s: absolute target %q", header.Name, header.Linkname)
			}
			resolved := filepath.Join(filepath.Dir(target), header.Linkname)
			if !isWithinDir(cleanDestPath, resolved) {
				return fmt.Errorf("invalid symlink %s: target %q escapes the feature directory", header.Name, header.Linkname)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
		}
	}

	if fileCount == 0 {
		return fmt.Errorf("tar archive contained no files")
	}

	return nil
}

// getRegistryToken obtains an authentication token from an OCI registry.
// It follows the Docker Registry v2 authentication spec.
func (r *Resolver) getRegistryToken(ctx context.Context, ref FeatureSource) (string, error) {
	// First, make an unauthenticated request to get the WWW-Authenticate header
	pingURL := fmt.Sprintf("%s://%s/v2/", ref.scheme(), ref.Registry)
	req, err := http.NewRequestWithContext(ctx, "GET", pingURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck // Close error irrelevant after read

	// If we got 200, no auth needed
	if resp.StatusCode == http.StatusOK {
		return "", nil
	}

	// Credentials from the docker config / credential helpers /
	// GITHUB_TOKEN (private registries); anonymous without them.
	creds, haveCreds, err := lookupCredentials(ctx, ref.Registry)
	if err != nil {
		return "", err
	}

	// Parse WWW-Authenticate header
	// Format: Bearer realm="https://...",service="...",scope="..."
	authHeader := resp.Header.Get("WWW-Authenticate")
	if authHeader == "" {
		return "", fmt.Errorf("no WWW-Authenticate header in response")
	}
	if strings.HasPrefix(strings.ToLower(authHeader), "basic") {
		// The registry takes the credentials on every request.
		if !haveCreds || creds.IdentityToken != "" {
			return "", fmt.Errorf("registry %s requires credentials (docker login %s)", ref.Registry, ref.Registry)
		}
		return basicAuthPrefix + base64.StdEncoding.EncodeToString([]byte(creds.Username+":"+creds.Password)), nil
	}

	// Parse the auth header
	realm, service := parseAuthHeader(authHeader)
	if realm == "" {
		return "", fmt.Errorf("failed to parse auth header: %s", authHeader)
	}

	// Build scope for the specific repository
	scope := fmt.Sprintf("repository:%s/%s:pull", ref.Repository, ref.Resource)

	// Request token
	tokenURL, err := url.Parse(realm)
	if err != nil {
		return "", fmt.Errorf("invalid token realm %q: %w", realm, err)
	}
	query := tokenURL.Query()
	query.Set("service", service)
	query.Set("scope", scope)
	tokenURL.RawQuery = query.Encode()
	var tokenReq *http.Request
	if creds.IdentityToken != "" {
		// An identity (refresh) token is exchanged with an OAuth2 POST.
		form := url.Values{
			"client_id":     {"dcx"},
			"grant_type":    {"refresh_token"},
			"service":       {service},
			"scope":         {scope},
			"refresh_token": {creds.IdentityToken},
		}
		tokenReq, err = http.NewRequestWithContext(ctx, "POST", realm, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		tokenReq, err = http.NewRequestWithContext(ctx, "GET", tokenURL.String(), nil)
		if err != nil {
			return "", err
		}
		if haveCreds {
			tokenReq.SetBasicAuth(creds.Username, creds.Password)
		}
	}

	tokenResp, err := httpClient.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("failed to request token: %w", err)
	}
	defer tokenResp.Body.Close() //nolint:errcheck // Close error irrelevant after read

	if tokenResp.StatusCode != http.StatusOK {
		body, _ := readLimited(tokenResp.Body, maxErrorBody)
		return "", fmt.Errorf("token request failed with %d: %s", tokenResp.StatusCode, string(body))
	}

	// Parse token response
	var tokenData struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(tokenResp.Body, maxManifestSize)).Decode(&tokenData); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	// Some registries return "token", others return "access_token"
	token := tokenData.Token
	if token == "" {
		token = tokenData.AccessToken
	}

	return token, nil
}

// lookupCredentials finds a registry's credentials (tests replace it).
var lookupCredentials = registryauth.Lookup

// basicAuthPrefix marks a getRegistryToken result that is Basic
// credentials rather than a Bearer token.
const basicAuthPrefix = "Basic "

// authorization renders getRegistryToken's result as an Authorization
// header value.
func authorization(token string) string {
	if strings.HasPrefix(token, basicAuthPrefix) {
		return token
	}
	return "Bearer " + token
}

// parseAuthHeader parses a WWW-Authenticate header to extract realm and service.
func parseAuthHeader(header string) (realm, service string) {
	// Remove "Bearer " prefix
	header = strings.TrimPrefix(header, "Bearer ")

	// Parse key="value" pairs
	pairs := strings.Split(header, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), "\"")

		switch key {
		case "realm":
			realm = value
		case "service":
			service = value
		}
	}
	return
}

// isWithinDir reports whether p is dir itself or lies beneath it (lexically).
func isWithinDir(dir, p string) bool {
	return p == dir || strings.HasPrefix(p, dir+string(os.PathSeparator))
}

// ensureNoSymlinkComponents rejects target if any existing path component
// between root (exclusive) and target (exclusive) is a symlink. Combined with
// validating symlink targets lexically, this keeps extraction inside root.
func ensureNoSymlinkComponents(root, target string) error {
	rel, err := filepath.Rel(root, filepath.Dir(target))
	if err != nil || rel == "." {
		return nil
	}
	cur := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %s is a symlink", cur)
		}
	}
	return nil
}
