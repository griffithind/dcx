package features

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/lockfile"
)

// Manager handles feature resolution, ordering, and installation.
type Manager struct {
	resolver  *Resolver
	configDir string
	lockfile  *lockfile.Lockfile
}

// NewManager creates a new feature manager.
func NewManager(configDir string) (*Manager, error) {
	resolver, err := NewResolver(configDir)
	if err != nil {
		return nil, err
	}

	return &Manager{
		resolver:  resolver,
		configDir: configDir,
	}, nil
}

// SetForcePull configures the manager to force re-fetch features from the registry.
func (m *Manager) SetForcePull(forcePull bool) {
	m.resolver.SetForcePull(forcePull)
}

// SetLockfile sets the lockfile to use for feature resolution.
// When set, features will be resolved using pinned versions from the lockfile.
func (m *Manager) SetLockfile(lf *lockfile.Lockfile) {
	m.lockfile = lf
}

// ResolveAll resolves all features from a devcontainer.json features map.
// It recursively resolves dependencies specified in dependsOn and installsAfter.
// If a lockfile is set via SetLockfile, pinned versions will be used.
func (m *Manager) ResolveAll(ctx context.Context, featuresConfig map[string]interface{}, overrideOrder []string) ([]*Feature, error) {
	if len(featuresConfig) == 0 {
		return nil, nil
	}

	// Track resolved features by their metadata ID
	resolved := make(map[string]*Feature)

	// Resolve each feature from config
	for id, optionsRaw := range featuresConfig {
		// Parse options
		var options map[string]interface{}
		switch v := optionsRaw.(type) {
		case map[string]interface{}:
			options = v
		case bool:
			// Boolean true means use defaults
			if !v {
				continue // Skip disabled features
			}
			options = make(map[string]interface{})
		default:
			options = make(map[string]interface{})
		}

		// Resolve the feature (with lockfile if set)
		feature, err := m.resolver.ResolveWithLockfile(ctx, id, options, m.lockfile)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve feature %q: %w", id, err)
		}

		// Use metadata ID as key if available
		key := id
		if feature.Metadata != nil && feature.Metadata.ID != "" {
			key = feature.Metadata.ID
		}
		resolved[key] = feature
	}

	// Recursively resolve dependencies
	if err := m.resolveDependencies(ctx, resolved); err != nil {
		return nil, err
	}

	// Convert map to slice
	features := make([]*Feature, 0, len(resolved))
	for _, f := range resolved {
		features = append(features, f)
	}

	// Order features (ValidateDependencies is no longer needed since we auto-resolved deps)
	ordered, err := OrderFeatures(features, overrideOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to order features: %w", err)
	}

	return ordered, nil
}

// resolveDependencies recursively resolves all dependencies for the given features.
func (m *Manager) resolveDependencies(ctx context.Context, resolved map[string]*Feature) error {
	// Collect all unresolved dependencies
	for {
		unresolved := m.collectUnresolvedDependencies(resolved)
		if len(unresolved) == 0 {
			break
		}

		// Resolve each unresolved dependency (with lockfile if set)
		for depID, depOptions := range unresolved {
			feature, err := m.resolver.ResolveWithLockfile(ctx, depID, depOptions, m.lockfile)
			if err != nil {
				return fmt.Errorf("failed to resolve dependency %q: %w", depID, err)
			}

			// Use metadata ID as key if available
			key := depID
			if feature.Metadata != nil && feature.Metadata.ID != "" {
				key = feature.Metadata.ID
			}
			resolved[key] = feature
		}
	}

	return nil
}

// collectUnresolvedDependencies finds all dependencies that haven't been resolved yet.
func (m *Manager) collectUnresolvedDependencies(resolved map[string]*Feature) map[string]map[string]interface{} {
	unresolved := make(map[string]map[string]interface{})

	// Helper to check if a dependency is already resolved
	// Dependencies can be specified as full OCI paths or short IDs
	isResolved := func(depID string) bool {
		// Check exact match first
		if _, exists := resolved[depID]; exists {
			return true
		}
		// Check if any resolved feature's metadata ID matches the dependency
		// This handles cases where depID is "ghcr.io/.../common-utils" but
		// the feature is stored under its metadata ID "common-utils"
		for _, f := range resolved {
			if f.Metadata != nil && f.Metadata.ID != "" {
				// Check if the dependency ends with the metadata ID
				// e.g., "ghcr.io/devcontainers/features/common-utils" ends with "common-utils"
				if f.Metadata.ID == depID {
					return true
				}
				// Also check if the full OCI path ends with /metadataID
				if len(depID) > len(f.Metadata.ID) && depID[len(depID)-len(f.Metadata.ID)-1] == '/' && depID[len(depID)-len(f.Metadata.ID):] == f.Metadata.ID {
					return true
				}
			}
		}
		return false
	}

	for _, feature := range resolved {
		if feature.Metadata == nil {
			continue
		}

		// Check hard dependencies (dependsOn)
		for depID, depOptionsRaw := range feature.Metadata.DependsOn {
			if !isResolved(depID) {
				// Parse options from dependency config
				var depOptions map[string]interface{}
				if opts, ok := depOptionsRaw.(map[string]interface{}); ok {
					depOptions = opts
				} else {
					depOptions = make(map[string]interface{})
				}
				unresolved[depID] = depOptions
			}
		}

		// Check soft dependencies (installsAfter) - these are also resolved if specified
		for _, depID := range feature.Metadata.InstallsAfter {
			if !isResolved(depID) {
				// Soft dependencies don't have options specified
				if _, alreadyQueued := unresolved[depID]; !alreadyQueued {
					unresolved[depID] = make(map[string]interface{})
				}
			}
		}
	}

	return unresolved
}
