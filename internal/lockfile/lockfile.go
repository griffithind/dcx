// Package lockfile provides types and utilities for parsing, validating,
// and working with devcontainer-lock.json files.
package lockfile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Lockfile represents the devcontainer-lock.json structure.
// It pins exact feature versions for reproducible builds.
//
// The lockfile format follows the devcontainer specification:
// https://github.com/devcontainers/spec/blob/main/docs/specs/devcontainer-lockfile.md
type Lockfile struct {
	// Features maps normalized feature IDs to their locked versions.
	// Feature IDs are normalized to lowercase for consistent lookups.
	Features map[string]LockedFeature `json:"features"`
}

// LockedFeature represents a locked feature entry in the lockfile.
type LockedFeature struct {
	// Version is the full semantic version (e.g., "2.5.5").
	Version string `json:"version"`

	// Resolved is the fully qualified reference with digest.
	// For OCI features: "ghcr.io/devcontainers/features/common-utils@sha256:abc123..."
	// For tarball features: the HTTPS URL
	Resolved string `json:"resolved"`

	// Integrity is the SHA256 checksum of the feature tarball.
	// Format: "sha256:hexstring"
	Integrity string `json:"integrity"`

	// DependsOn lists hard dependencies (feature IDs from dependsOn field).
	// Empty array is omitted.
	DependsOn []string `json:"dependsOn,omitempty"`
}

// Load loads a lockfile from the configuration directory.
// It returns:
//   - lockfile: the parsed lockfile, or nil if not found
//   - initLockfile: true if an empty file exists (marker for initialization)
//   - error: any parsing or I/O error
//
// Per the spec, an empty file signals "initialize lockfile on build completion".
func Load(configPath string) (*Lockfile, bool, error) {
	lockfilePath := GetPath(configPath)

	data, err := os.ReadFile(lockfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil // No lockfile exists
		}
		return nil, false, fmt.Errorf("failed to read lockfile: %w", err)
	}

	// Empty file is a marker for initialization
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, true, nil
	}

	var lockfile Lockfile
	if err := json.Unmarshal(data, &lockfile); err != nil {
		return nil, false, fmt.Errorf("failed to parse lockfile: %w", err)
	}

	// Ensure Features map is initialized
	if lockfile.Features == nil {
		lockfile.Features = make(map[string]LockedFeature)
	}

	return &lockfile, false, nil
}

// Save writes the lockfile to disk.
func (l *Lockfile) Save(configPath string) error {
	lockfilePath := GetPath(configPath)

	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal lockfile: %w", err)
	}

	// Add trailing newline
	data = append(data, '\n')

	if err := os.WriteFile(lockfilePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write lockfile: %w", err)
	}

	return nil
}

// GetPath returns the lockfile path based on the config file naming.
// Per the spec:
//   - .devcontainer.json → .devcontainer-lock.json
//   - devcontainer.json → devcontainer-lock.json
func GetPath(configPath string) string {
	dir := filepath.Dir(configPath)
	base := filepath.Base(configPath)

	var lockfileName string
	if strings.HasPrefix(base, ".") {
		lockfileName = ".devcontainer-lock.json"
	} else {
		lockfileName = "devcontainer-lock.json"
	}

	return filepath.Join(dir, lockfileName)
}

// NormalizeFeatureID converts a feature ID to lowercase for consistent lookups.
// Per the spec, feature identifiers are case-insensitive.
func NormalizeFeatureID(id string) string {
	return strings.ToLower(id)
}

// Get retrieves a locked feature by its ID (case-insensitive lookup).
func (l *Lockfile) Get(featureID string) (LockedFeature, bool) {
	if l == nil || l.Features == nil {
		return LockedFeature{}, false
	}
	locked, ok := l.Features[NormalizeFeatureID(featureID)]
	return locked, ok
}

// Set adds or updates a locked feature.
func (l *Lockfile) Set(featureID string, locked LockedFeature) {
	if l.Features == nil {
		l.Features = make(map[string]LockedFeature)
	}
	l.Features[NormalizeFeatureID(featureID)] = locked
}

// New creates an empty lockfile.
func New() *Lockfile {
	return &Lockfile{
		Features: make(map[string]LockedFeature),
	}
}

// IsEmpty returns true if the lockfile has no features.
func (l *Lockfile) IsEmpty() bool {
	return l == nil || len(l.Features) == 0
}

// Equals compares two lockfiles for equality.
// Used for detecting if lockfile needs to be updated.
func (l *Lockfile) Equals(other *Lockfile) bool {
	if l == nil && other == nil {
		return true
	}
	if l == nil || other == nil {
		return false
	}
	if len(l.Features) != len(other.Features) {
		return false
	}
	for id, locked := range l.Features {
		otherLocked, ok := other.Features[id]
		if !ok {
			return false
		}
		if locked.Version != otherLocked.Version ||
			locked.Resolved != otherLocked.Resolved ||
			locked.Integrity != otherLocked.Integrity {
			return false
		}
		// Compare DependsOn arrays
		if len(locked.DependsOn) != len(otherLocked.DependsOn) {
			return false
		}
		for i, dep := range locked.DependsOn {
			if dep != otherLocked.DependsOn[i] {
				return false
			}
		}
	}
	return true
}
