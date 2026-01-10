package features

import (
	"fmt"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/lockfile"
)

// GenerateLockfile creates a lockfile from resolved features.
// Local features (./path) are excluded per the devcontainer specification.
func GenerateLockfile(features []*Feature) *lockfile.Lockfile {
	lf := lockfile.New()

	for _, f := range features {
		// Skip local features per spec
		if f.Source.Type == SourceTypeLocalPath {
			continue
		}

		// Skip features without integrity info (shouldn't happen but be safe)
		if f.Integrity == "" {
			continue
		}

		normalizedID := lockfile.NormalizeFeatureID(f.ID)

		var resolved string
		switch f.Source.Type {
		case SourceTypeOCI:
			// Format: registry/repository/resource@sha256:...
			if f.ManifestDigest != "" {
				resolved = fmt.Sprintf("%s/%s/%s@%s",
					f.Source.Registry, f.Source.Repository,
					f.Source.Resource, f.ManifestDigest)
			} else {
				// Fallback to version tag if no digest
				resolved = fmt.Sprintf("%s/%s/%s:%s",
					f.Source.Registry, f.Source.Repository,
					f.Source.Resource, f.Source.Version)
			}
		case SourceTypeTarball:
			// For HTTP tarballs, use the URL as resolved
			resolved = f.Source.URL
		}

		// Extract version from metadata
		version := ""
		if f.Metadata != nil {
			version = f.Metadata.Version
		}

		// Extract dependencies
		var dependsOn []string
		if f.Metadata != nil && len(f.Metadata.DependsOn) > 0 {
			dependsOn = extractDependencies(f.Metadata.DependsOn)
		}

		lf.Set(normalizedID, lockfile.LockedFeature{
			Version:   version,
			Resolved:  resolved,
			Integrity: f.Integrity,
			DependsOn: dependsOn,
		})
	}

	return lf
}

// extractDependencies extracts feature IDs from the dependsOn map.
func extractDependencies(dependsOn map[string]interface{}) []string {
	if len(dependsOn) == 0 {
		return nil
	}

	deps := make([]string, 0, len(dependsOn))
	for id := range dependsOn {
		deps = append(deps, strings.ToLower(id))
	}

	// Sort for consistent output
	sort.Strings(deps)
	return deps
}

// VerifyLockfile compares resolved features against an existing lockfile.
// Returns a list of mismatches if any.
func VerifyLockfile(features []*Feature, lf *lockfile.Lockfile) []LockfileMismatch {
	if lf == nil {
		return nil
	}

	var mismatches []LockfileMismatch

	// Check each feature against lockfile
	for _, f := range features {
		// Skip local features
		if f.Source.Type == SourceTypeLocalPath {
			continue
		}

		normalizedID := lockfile.NormalizeFeatureID(f.ID)
		locked, ok := lf.Get(normalizedID)

		if !ok {
			mismatches = append(mismatches, LockfileMismatch{
				FeatureID: f.ID,
				Type:      MismatchMissing,
				Message:   fmt.Sprintf("feature %s not found in lockfile", f.ID),
			})
			continue
		}

		// Check version
		version := ""
		if f.Metadata != nil {
			version = f.Metadata.Version
		}
		if version != "" && locked.Version != "" && version != locked.Version {
			mismatches = append(mismatches, LockfileMismatch{
				FeatureID: f.ID,
				Type:      MismatchVersion,
				Message:   fmt.Sprintf("version mismatch: lockfile has %s, resolved %s", locked.Version, version),
			})
		}

		// Check integrity
		if f.Integrity != "" && locked.Integrity != "" && f.Integrity != locked.Integrity {
			mismatches = append(mismatches, LockfileMismatch{
				FeatureID: f.ID,
				Type:      MismatchIntegrity,
				Message:   fmt.Sprintf("integrity mismatch: lockfile has %s, resolved %s", locked.Integrity, f.Integrity),
			})
		}
	}

	// Check for features in lockfile that aren't in resolved features
	for id := range lf.Features {
		found := false
		for _, f := range features {
			if f.Source.Type == SourceTypeLocalPath {
				continue
			}
			if lockfile.NormalizeFeatureID(f.ID) == id {
				found = true
				break
			}
		}
		if !found {
			mismatches = append(mismatches, LockfileMismatch{
				FeatureID: id,
				Type:      MismatchExtra,
				Message:   fmt.Sprintf("feature %s in lockfile but not in devcontainer.json", id),
			})
		}
	}

	return mismatches
}

// LockfileMismatch represents a mismatch between lockfile and resolved features.
type LockfileMismatch struct {
	FeatureID string
	Type      MismatchType
	Message   string
}

// MismatchType indicates the type of lockfile mismatch.
type MismatchType string

const (
	MismatchMissing   MismatchType = "missing"   // Feature not in lockfile
	MismatchExtra     MismatchType = "extra"     // Feature in lockfile but not resolved
	MismatchVersion   MismatchType = "version"   // Version mismatch
	MismatchIntegrity MismatchType = "integrity" // Integrity mismatch
)

// IsOutdated returns true if there are any mismatches.
func IsOutdated(mismatches []LockfileMismatch) bool {
	return len(mismatches) > 0
}

// NeedsUpdate returns true if the lockfile needs to be updated.
// This is true if there are missing or extra features, or version/integrity mismatches.
func NeedsUpdate(mismatches []LockfileMismatch) bool {
	for _, m := range mismatches {
		switch m.Type {
		case MismatchMissing, MismatchExtra, MismatchVersion, MismatchIntegrity:
			return true
		}
	}
	return false
}
