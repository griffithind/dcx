package features

import (
	"context"
	"fmt"
	"github.com/griffithind/dcx/internal/report"
	"path"
	"strings"

	"github.com/griffithind/dcx/internal/lockfile"
)

// maxDependencyPasses bounds dependency resolution. Real dependency chains
// are a handful of levels deep; hitting this means the graph never converges.
const maxDependencyPasses = 32

// Manager handles feature resolution, ordering, and installation.
type Manager struct {
	resolver  *Resolver
	configDir string
	lockfile  *lockfile.Lockfile

	// resolveFeature resolves one feature reference. Defaults to the
	// resolver (honouring the lockfile); overridable in tests.
	resolveFeature func(ctx context.Context, id string, options map[string]interface{}) (*Feature, error)
}

// NewManager creates a new feature manager.
func NewManager(configDir string, rep report.Reporter) (*Manager, error) {
	resolver, err := NewResolver(configDir, rep)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		resolver:  resolver,
		configDir: configDir,
	}
	m.resolveFeature = func(ctx context.Context, id string, options map[string]interface{}) (*Feature, error) {
		return m.resolver.ResolveWithLockfile(ctx, id, options, m.lockfile)
	}
	return m, nil
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
		case string:
			// A string is the "version" option ("…/node:1": "18"), as in
			// the reference implementation.
			options = map[string]interface{}{"version": v}
		default:
			options = make(map[string]interface{})
		}

		// Resolve the feature (with lockfile if set)
		feature, err := m.resolveFeature(ctx, id, options)
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
//
// Each dependency reference is resolved at most once. Without that guard a
// reference that never matches its resolved feature (e.g. a versioned
// "ghcr.io/.../pipx-package:1" key) was re-resolved forever.
func (m *Manager) resolveDependencies(ctx context.Context, resolved map[string]*Feature) error {
	attempted := make(map[string]bool)

	for pass := 0; ; pass++ {
		unresolved := m.collectUnresolvedDependencies(resolved)
		for depID := range unresolved {
			if attempted[depID] {
				delete(unresolved, depID)
			}
		}
		if len(unresolved) == 0 {
			return nil
		}
		if pass >= maxDependencyPasses {
			return fmt.Errorf("feature dependencies did not converge after %d passes (unresolved: %v)", maxDependencyPasses, mapKeys(unresolved))
		}

		// Resolve each unresolved dependency (with lockfile if set)
		for depID, depOptions := range unresolved {
			attempted[depID] = true
			feature, err := m.resolveFeature(ctx, depID, depOptions)
			if err != nil {
				return fmt.Errorf("failed to resolve dependency %q: %w", depID, err)
			}

			// Use metadata ID as key if available
			key := depID
			if feature.Metadata != nil && feature.Metadata.ID != "" {
				key = feature.Metadata.ID
			}
			// Never let a dependency overwrite a feature (and its options)
			// that is already resolved under the same ID.
			if _, exists := resolved[key]; !exists {
				resolved[key] = feature
			}
		}
	}
}

func mapKeys(m map[string]map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// normalizeFeatureRef reduces a feature reference to the bare feature name so
// references can be compared regardless of registry, version tag or digest:
//
//	ghcr.io/devcontainers/features/node:1      -> node
//	ghcr.io/devcontainers/features/node@sha256:… -> node
//	./local-feature                             -> local-feature
//	https://example.com/feature.tgz             -> feature
func normalizeFeatureRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	ref = strings.TrimRight(ref, "/")
	name := path.Base(ref)
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = name[:i]
	}
	name = strings.TrimSuffix(name, ".tgz")
	name = strings.TrimSuffix(name, ".tar.gz")
	return name
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
		// Dependencies can be full OCI refs (with tag or digest), local paths
		// or tarball URLs, while resolved features are keyed by metadata ID.
		want := normalizeFeatureRef(depID)
		for key, f := range resolved {
			if key == depID || normalizeFeatureRef(key) == want {
				return true
			}
			if f.ID != "" && (f.ID == depID || normalizeFeatureRef(f.ID) == want) {
				return true
			}
			if f.Metadata != nil && f.Metadata.ID == want {
				return true
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

		// installsAfter is only an ordering hint (spec): it never adds a
		// feature the user did not ask for.
	}

	return unresolved
}
