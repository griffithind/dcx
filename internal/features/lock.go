package features

import (
	"fmt"
	"slices"

	"github.com/griffithind/dcx/internal/lockfile"
)

// GenerateLockfile creates a lockfile from resolved features, as
// devcontainers/cli writes it: each OCI or tarball feature keyed by its
// reference as written, with its version, the resolved reference, its
// integrity (the OCI manifest digest, or the tarball's SHA256) and its
// dependsOn references in declaration order. Local features are not
// locked.
func GenerateLockfile(features []*Feature) *lockfile.Lockfile {
	lf := lockfile.New()
	for _, f := range features {
		if entry, ok := lockedEntry(f); ok {
			lf.Set(f.ID, entry)
		}
	}
	return lf
}

// lockedEntry is f's lockfile entry; ok is false for a feature that is not
// locked (local, or without a digest).
func lockedEntry(f *Feature) (lockfile.LockedFeature, bool) {
	var entry lockfile.LockedFeature
	switch f.Source.Type {
	case SourceTypeOCI:
		if f.ManifestDigest == "" {
			return entry, false
		}
		entry.Integrity = f.ManifestDigest
		entry.Resolved = fmt.Sprintf("%s/%s/%s@%s", f.Source.Registry, f.Source.Repository, f.Source.Resource, f.ManifestDigest)
	case SourceTypeTarball:
		if f.Integrity == "" {
			return entry, false
		}
		entry.Integrity = f.Integrity
		entry.Resolved = f.Source.URL
	default:
		return entry, false
	}
	if f.Metadata != nil {
		entry.Version = f.Metadata.Version
		for _, dep := range f.Metadata.DependsOn {
			entry.DependsOn = append(entry.DependsOn, dep.ID)
		}
	}
	return entry, true
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
		entry, ok := lockedEntry(f)
		if !ok {
			continue
		}
		locked, ok := lf.Get(f.ID)
		if !ok {
			mismatches = append(mismatches, LockfileMismatch{
				FeatureID: f.ID,
				Type:      MismatchMissing,
				Message:   fmt.Sprintf("feature %s not found in lockfile", f.ID),
			})
			continue
		}
		if entry.Version != "" && locked.Version != "" && entry.Version != locked.Version {
			mismatches = append(mismatches, LockfileMismatch{
				FeatureID: f.ID,
				Type:      MismatchVersion,
				Message:   fmt.Sprintf("%s: version mismatch: lockfile has %s, resolved %s", f.ID, locked.Version, entry.Version),
			})
		}
		if locked.Integrity != entry.Integrity {
			mismatches = append(mismatches, LockfileMismatch{
				FeatureID: f.ID,
				Type:      MismatchIntegrity,
				Message:   fmt.Sprintf("%s: integrity mismatch: lockfile has %s, resolved %s", f.ID, locked.Integrity, entry.Integrity),
			})
		}
	}

	// Check for features in lockfile that aren't in resolved features
	for id := range lf.Features {
		if !slices.ContainsFunc(features, func(f *Feature) bool { return f.ID == id && f.Source.Type != SourceTypeLocalPath }) {
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
