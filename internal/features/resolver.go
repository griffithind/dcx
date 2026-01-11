package features

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/lockfile"
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
}

// DigestInfo holds digest information for a resolved feature.
type DigestInfo struct {
	ManifestDigest string `json:"manifest_digest,omitempty"` // OCI manifest digest
	Integrity      string `json:"integrity"`                 // Tarball SHA256 hash
}

const digestFileName = ".dcx-integrity"

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

// extractDigestFromResolved extracts the manifest digest from a lockfile resolved field.
// The resolved field format is: registry/repository/resource@sha256:...
// Returns empty string if no digest is present (e.g., for tarball URLs or tag references).
func extractDigestFromResolved(resolved string) string {
	if resolved == "" {
		return ""
	}
	// Look for @sha256: or @sha384: or @sha512: pattern
	atIndex := strings.LastIndex(resolved, "@")
	if atIndex == -1 {
		return ""
	}
	digest := resolved[atIndex+1:]
	// Validate it looks like a digest (starts with sha256:, sha384:, or sha512:)
	if strings.HasPrefix(digest, "sha256:") ||
		strings.HasPrefix(digest, "sha384:") ||
		strings.HasPrefix(digest, "sha512:") {
		return digest
	}
	return ""
}

// NewResolver creates a new feature resolver.
func NewResolver(configDir string) (*Resolver, error) {
	// Determine cache directory
	cacheDir, err := getCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to determine cache directory: %w", err)
	}

	return &Resolver{
		cacheDir:  cacheDir,
		configDir: configDir,
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

// Resolve resolves a feature from its ID and options.
func (r *Resolver) Resolve(ctx context.Context, id string, options map[string]interface{}) (*Feature, error) {
	return r.ResolveWithLockfile(ctx, id, options, nil)
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

// resolveLocal resolves a local feature.
func (r *Resolver) resolveLocal(ctx context.Context, feature *Feature) error {
	// Resolve path relative to config directory
	path := feature.Source.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.configDir, path)
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

// resolveOCIWithLockfile resolves an OCI feature, optionally using lockfile for pinned versions.
func (r *Resolver) resolveOCIWithLockfile(ctx context.Context, feature *Feature, lockfile *lockfile.Lockfile) error {
	ref := feature.Source

	// Check if we have a locked version
	var expectedIntegrity string
	var lockedManifestDigest string
	if lockfile != nil {
		if locked, ok := lockfile.Get(feature.ID); ok {
			expectedIntegrity = locked.Integrity
			// Extract manifest digest from Resolved field (format: registry/path@sha256:...)
			lockedManifestDigest = extractDigestFromResolved(locked.Resolved)
		}
	}

	// Compute cache key
	cacheKey := computeCacheKey(ref.CanonicalID())
	cachePath := filepath.Join(r.cacheDir, cacheKey)

	// Check if already cached (unless force-pull is enabled)
	if !r.forcePull {
		if _, err := os.Stat(cachePath); err == nil {
			feature.CachePath = cachePath
			metadata, err := r.loadMetadata(cachePath)
			if err != nil {
				return fmt.Errorf("failed to load cached feature metadata: %w", err)
			}
			feature.Metadata = metadata

			// Load and populate digest info from cache
			if digestInfo, err := loadDigestInfo(cachePath); err == nil {
				feature.ManifestDigest = digestInfo.ManifestDigest
				feature.Integrity = digestInfo.Integrity

				// Verify integrity against lockfile if available
				if expectedIntegrity != "" && digestInfo.Integrity != expectedIntegrity {
					// Cache integrity doesn't match lockfile, need to re-fetch
					fmt.Printf("    Cache integrity mismatch for %s, re-fetching...\n", ref.CanonicalID())
					_ = os.RemoveAll(cachePath)
				} else {
					return nil
				}
			}
			// If no digest file exists, continue to use cached version
			// (backwards compatibility with pre-lockfile caches)
			if expectedIntegrity == "" {
				return nil
			}
		}
	} else {
		// Force-pull: remove existing cache to re-fetch
		_ = os.RemoveAll(cachePath)
	}

	// Fetch from OCI registry
	if lockedManifestDigest != "" {
		fmt.Printf("    Fetching feature from registry: %s (locked to %s)\n", ref.CanonicalID(), lockedManifestDigest[:min(19, len(lockedManifestDigest))]+"...")
	} else {
		fmt.Printf("    Fetching feature from registry: %s\n", ref.CanonicalID())
	}
	digestInfo, err := r.fetchOCIWithDigest(ctx, ref, cachePath, lockedManifestDigest, expectedIntegrity)
	if err != nil {
		return fmt.Errorf("failed to fetch OCI feature: %w", err)
	}

	feature.CachePath = cachePath
	feature.ManifestDigest = digestInfo.ManifestDigest
	feature.Integrity = digestInfo.Integrity

	// Load metadata
	metadata, err := r.loadMetadata(cachePath)
	if err != nil {
		return fmt.Errorf("failed to load feature metadata: %w", err)
	}
	feature.Metadata = metadata

	return nil
}

// resolveHTTPWithLockfile resolves an HTTP feature, optionally using lockfile for integrity verification.
func (r *Resolver) resolveHTTPWithLockfile(ctx context.Context, feature *Feature, lockfile *lockfile.Lockfile) error {
	ref := feature.Source

	// Check if we have a locked version
	var expectedIntegrity string
	if lockfile != nil {
		if locked, ok := lockfile.Get(feature.ID); ok {
			expectedIntegrity = locked.Integrity
		}
	}

	// Compute cache key
	cacheKey := computeCacheKey(ref.URL)
	cachePath := filepath.Join(r.cacheDir, cacheKey)

	// Check if already cached (unless force-pull is enabled)
	if !r.forcePull {
		if _, err := os.Stat(cachePath); err == nil {
			feature.CachePath = cachePath
			metadata, err := r.loadMetadata(cachePath)
			if err != nil {
				return fmt.Errorf("failed to load cached feature metadata: %w", err)
			}
			feature.Metadata = metadata

			// Load and populate digest info from cache
			if digestInfo, err := loadDigestInfo(cachePath); err == nil {
				feature.Integrity = digestInfo.Integrity

				// Verify integrity against lockfile if available
				if expectedIntegrity != "" && digestInfo.Integrity != expectedIntegrity {
					// Cache integrity doesn't match lockfile, need to re-fetch
					fmt.Printf("    Cache integrity mismatch for %s, re-fetching...\n", ref.URL)
					_ = os.RemoveAll(cachePath)
				} else {
					return nil
				}
			}
			// If no digest file exists, continue to use cached version
			if expectedIntegrity == "" {
				return nil
			}
		}
	} else {
		// Force-pull: remove existing cache to re-fetch
		_ = os.RemoveAll(cachePath)
	}

	// Fetch from HTTP
	integrity, err := r.fetchHTTPWithDigest(ctx, ref.URL, cachePath, expectedIntegrity)
	if err != nil {
		return fmt.Errorf("failed to fetch HTTP feature: %w", err)
	}

	feature.CachePath = cachePath
	feature.Integrity = integrity

	// Load metadata
	metadata, err := r.loadMetadata(cachePath)
	if err != nil {
		return fmt.Errorf("failed to load feature metadata: %w", err)
	}
	feature.Metadata = metadata

	return nil
}

// fetchOCIWithDigest fetches a feature from an OCI registry and returns digest info.
// If lockedManifestDigest is provided (from lockfile), it fetches the manifest by digest
// instead of by tag, ensuring exact reproducibility.
func (r *Resolver) fetchOCIWithDigest(ctx context.Context, ref FeatureSource, destPath string, lockedManifestDigest string, expectedIntegrity string) (*DigestInfo, error) {
	// Build the OCI manifest URL
	// For ghcr.io, the format is: https://ghcr.io/v2/{repository}/{feature}/manifests/{tag_or_digest}
	// When we have a locked manifest digest, use it instead of the tag for exact reproducibility
	manifestReference := ref.Version
	if lockedManifestDigest != "" {
		manifestReference = lockedManifestDigest
	}
	manifestURL := fmt.Sprintf("https://%s/v2/%s/%s/manifests/%s",
		ref.Registry, ref.Repository, ref.Resource, manifestReference)

	// Get token for authentication (required for most OCI registries)
	token, err := r.getRegistryToken(ctx, ref)
	if err != nil {
		// Continue without token - some registries might not require auth
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
		req.Header.Set("Authorization", "Bearer "+token)
	}

	// Make request
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // Close error irrelevant after read

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	// Read manifest body for digest computation
	manifestBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	// Capture manifest digest from header or compute it
	manifestDigest := resp.Header.Get("Docker-Content-Digest")
	if manifestDigest == "" {
		manifestDigest = computeIntegrity(manifestBody)
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
	blobURL := fmt.Sprintf("https://%s/v2/%s/%s/blobs/%s",
		ref.Registry, ref.Repository, ref.Resource, featureLayer.Digest)

	blobReq, err := http.NewRequestWithContext(ctx, "GET", blobURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		blobReq.Header.Set("Authorization", "Bearer "+token)
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
	bodyData, err := io.ReadAll(blobResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read blob body: %w", err)
	}

	// Compute tarball integrity
	integrity := computeIntegrity(bodyData)

	// Verify integrity against expected if provided
	if err := verifyIntegrity(bodyData, expectedIntegrity); err != nil {
		return nil, fmt.Errorf("feature %s/%s/%s: %w", ref.Registry, ref.Repository, ref.Resource, err)
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
	if err := saveDigestInfo(destPath, *digestInfo); err != nil {
		// Log but don't fail - digest info is nice to have
		fmt.Printf("    Warning: failed to save digest info: %v\n", err)
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
	bodyData, err := io.ReadAll(resp.Body)
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
	if err := saveDigestInfo(destPath, digestInfo); err != nil {
		// Log but don't fail
		fmt.Printf("    Warning: failed to save digest info: %v\n", err)
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
func extractTarReader(tr *tar.Reader, destPath string) error {
	cleanDestPath := filepath.Clean(destPath)
	fileCount := 0

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
		target := filepath.Join(destPath, cleanName)
		if !strings.HasPrefix(target, cleanDestPath+string(os.PathSeparator)) && target != cleanDestPath {
			return fmt.Errorf("invalid tar path: %s", header.Name)
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

			// Create file
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			_ = f.Close()
		case tar.TypeSymlink:
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
	pingURL := fmt.Sprintf("https://%s/v2/", ref.Registry)
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

	// Parse WWW-Authenticate header
	// Format: Bearer realm="https://...",service="...",scope="..."
	authHeader := resp.Header.Get("WWW-Authenticate")
	if authHeader == "" {
		return "", fmt.Errorf("no WWW-Authenticate header in response")
	}

	// Parse the auth header
	realm, service := parseAuthHeader(authHeader)
	if realm == "" {
		return "", fmt.Errorf("failed to parse auth header: %s", authHeader)
	}

	// Build scope for the specific repository
	scope := fmt.Sprintf("repository:%s/%s:pull", ref.Repository, ref.Resource)

	// Request token
	tokenURL := fmt.Sprintf("%s?service=%s&scope=%s", realm, service, scope)
	tokenReq, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	if err != nil {
		return "", err
	}

	tokenResp, err := httpClient.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("failed to request token: %w", err)
	}
	defer tokenResp.Body.Close() //nolint:errcheck // Close error irrelevant after read

	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokenResp.Body)
		return "", fmt.Errorf("token request failed with %d: %s", tokenResp.StatusCode, string(body))
	}

	// Parse token response
	var tokenData struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		return "", fmt.Errorf("failed to parse token response: %w", err)
	}

	// Some registries return "token", others return "access_token"
	token := tokenData.Token
	if token == "" {
		token = tokenData.AccessToken
	}

	return token, nil
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
