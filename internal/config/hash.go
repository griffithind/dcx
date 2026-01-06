package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/gowebpki/jcs"
)

// HashConfig represents the data structure used for hash computation.
// This captures all elements that should trigger a rebuild when changed.
type HashConfig struct {
	// Raw devcontainer.json content (after stripping comments)
	DevcontainerJSON json.RawMessage `json:"devcontainer_json"`

	// Compose file contents (for compose plans)
	ComposeFiles map[string]string `json:"compose_files,omitempty"`

	// Dockerfile content (for build plans)
	DockerfileContent string `json:"dockerfile_content,omitempty"`

	// Features configuration
	Features map[string]interface{} `json:"features,omitempty"`

	// Schema version for invalidation on breaking changes
	SchemaVersion string `json:"schema_version"`
}

// Current schema version - increment when hash computation logic changes
const hashSchemaVersion = "1"

// ComputeHash computes a deterministic hash of the configuration.
// The hash is computed using RFC 8785 JSON Canonicalization Scheme.
func ComputeHash(cfg *DevcontainerConfig) (string, error) {
	hashConfig := HashConfig{
		SchemaVersion: hashSchemaVersion,
		Features:      cfg.Features,
	}

	// Include raw JSON if available
	if raw := cfg.GetRawJSON(); raw != nil {
		hashConfig.DevcontainerJSON = raw
	} else {
		// Fall back to re-marshaling
		data, err := json.Marshal(cfg)
		if err != nil {
			return "", fmt.Errorf("failed to marshal config: %w", err)
		}
		hashConfig.DevcontainerJSON = data
	}

	return computeHashFromStruct(hashConfig)
}

// ComputeHashWithFiles computes a hash including external file contents.
func ComputeHashWithFiles(cfg *DevcontainerConfig, configDir string) (string, error) {
	hashConfig := HashConfig{
		SchemaVersion: hashSchemaVersion,
		Features:      cfg.Features,
	}

	// Include raw JSON
	if raw := cfg.GetRawJSON(); raw != nil {
		hashConfig.DevcontainerJSON = raw
	} else {
		data, err := json.Marshal(cfg)
		if err != nil {
			return "", fmt.Errorf("failed to marshal config: %w", err)
		}
		hashConfig.DevcontainerJSON = data
	}

	// Include compose files for compose plans
	if cfg.IsComposePlan() {
		files := cfg.GetDockerComposeFiles()
		hashConfig.ComposeFiles = make(map[string]string, len(files))
		for _, f := range files {
			path := ResolveRelativePath(configDir, f)
			content, err := os.ReadFile(path)
			if err == nil {
				hashConfig.ComposeFiles[f] = string(content)
			}
		}
	}

	// Include Dockerfile for build plans
	if cfg.Build != nil && cfg.Build.Dockerfile != "" {
		path := ResolveRelativePath(configDir, cfg.Build.Dockerfile)
		content, err := os.ReadFile(path)
		if err == nil {
			hashConfig.DockerfileContent = string(content)
		}
	}

	return computeHashFromStruct(hashConfig)
}

func computeHashFromStruct(hashConfig HashConfig) (string, error) {
	// Marshal to JSON
	jsonBytes, err := json.Marshal(hashConfig)
	if err != nil {
		return "", fmt.Errorf("failed to marshal hash config: %w", err)
	}

	// Canonicalize using RFC 8785 JCS
	canonical, err := jcs.Transform(jsonBytes)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize JSON: %w", err)
	}

	// Compute SHA256 hash
	hash := sha256.Sum256(canonical)
	return hex.EncodeToString(hash[:]), nil
}

// ComputeSimpleHash computes a simple hash from a byte slice.
func ComputeSimpleHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

// ComputeMapHash computes a deterministic hash of a string map.
func ComputeMapHash(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}

	// Sort keys for determinism
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Build canonical representation
	var data []byte
	for _, k := range keys {
		data = append(data, k...)
		data = append(data, '=')
		data = append(data, m[k]...)
		data = append(data, '\n')
	}

	return ComputeSimpleHash(data)
}
