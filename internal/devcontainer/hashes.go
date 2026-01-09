package devcontainer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/features"
)

// ContentHashes contains hashes for staleness detection.
// These hashes are stored in container labels to detect when
// configuration has changed and a rebuild is needed.
type ContentHashes struct {
	// Config is the hash of the devcontainer.json content.
	Config string

	// Dockerfile is the hash of the Dockerfile content (if applicable).
	Dockerfile string

	// Compose is the hash of the docker-compose.yml content (if applicable).
	Compose string

	// Features is the combined hash of all resolved features.
	Features string

	// Overall is the combined hash of all above hashes.
	Overall string
}

// NewContentHashes creates a new empty ContentHashes.
func NewContentHashes() *ContentHashes {
	return &ContentHashes{}
}

// ComputeConfigHash computes the hash of a DevContainerConfig.
// Uses the raw JSON if available, otherwise marshals the config.
func ComputeConfigHash(cfg *DevContainerConfig) (string, error) {
	if raw := cfg.GetRawJSON(); len(raw) > 0 {
		return hashBytes(raw), nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal config for hash: %w", err)
	}
	return hashBytes(data), nil
}

// ComputeSimpleHash computes a simple SHA256 hash of raw JSON bytes.
// This is useful for quick hash computation without full config parsing.
func ComputeSimpleHash(data []byte) string {
	return hashBytes(data)
}

// ComputeDockerfileHash computes the hash of a Dockerfile.
func ComputeDockerfileHash(dockerfilePath string) (string, error) {
	content, err := os.ReadFile(dockerfilePath)
	if err != nil {
		return "", err
	}
	return hashBytes(content), nil
}

// ComputeComposeHash computes the combined hash of compose files.
func ComputeComposeHash(composeFiles []string) (string, error) {
	var combined []byte
	for _, f := range composeFiles {
		content, err := os.ReadFile(f)
		if err != nil {
			return "", fmt.Errorf("read compose file %s: %w", f, err)
		}
		combined = append(combined, content...)
	}
	if len(combined) == 0 {
		return "", nil
	}
	return hashBytes(combined), nil
}

// ComputeFeaturesHash computes the combined hash of resolved features.
// The hash includes feature IDs, versions, and options for accurate staleness detection.
func ComputeFeaturesHash(resolvedFeatures []*features.Feature) string {
	if len(resolvedFeatures) == 0 {
		return ""
	}

	var featureData []string
	for _, f := range resolvedFeatures {
		// Include ID, version, and options in hash
		optData, _ := json.Marshal(f.Options)
		version := ""
		if f.Metadata != nil {
			version = f.Metadata.Version
		}
		featureData = append(featureData, fmt.Sprintf("%s:%s:%s", f.ID, version, string(optData)))
	}
	sort.Strings(featureData)
	return hashBytes([]byte(strings.Join(featureData, "|")))
}

// ComputeOverallHash computes the combined hash of all configuration.
func ComputeOverallHash(hashes *ContentHashes) string {
	overall := fmt.Sprintf("%s|%s|%s|%s",
		hashes.Config,
		hashes.Dockerfile,
		hashes.Compose,
		hashes.Features)
	return hashBytes([]byte(overall))
}

// ComputeAllHashes computes all hashes for a resolved devcontainer.
// This is a convenience function that populates a ContentHashes struct.
func ComputeAllHashes(cfg *DevContainerConfig, dockerfilePath string, composeFiles []string, resolvedFeatures []*features.Feature) (*ContentHashes, error) {
	hashes := NewContentHashes()

	// Config hash
	configHash, err := ComputeConfigHash(cfg)
	if err != nil {
		return nil, err
	}
	hashes.Config = configHash

	// Dockerfile hash
	if dockerfilePath != "" {
		if hash, err := ComputeDockerfileHash(dockerfilePath); err == nil {
			hashes.Dockerfile = hash
		}
	}

	// Compose hash
	if len(composeFiles) > 0 {
		if hash, err := ComputeComposeHash(composeFiles); err == nil {
			hashes.Compose = hash
		}
	}

	// Features hash
	hashes.Features = ComputeFeaturesHash(resolvedFeatures)

	// Overall hash
	hashes.Overall = ComputeOverallHash(hashes)

	return hashes, nil
}

// IsStale checks if the current hashes indicate staleness compared to stored hashes.
func (h *ContentHashes) IsStale(stored *ContentHashes) bool {
	if stored == nil {
		return true
	}
	return h.Overall != stored.Overall
}

// hashBytes computes a SHA256 hash of the given data and returns it as a hex string.
func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
