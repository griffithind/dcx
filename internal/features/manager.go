package features

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Manager handles feature resolution, ordering, and installation.
type Manager struct {
	resolver  *Resolver
	configDir string
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

// ResolveAll resolves all features from a devcontainer.json features map.
// It recursively resolves dependencies specified in dependsOn and installsAfter.
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

		// Resolve the feature
		feature, err := m.resolver.Resolve(ctx, id, options)
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

		// Resolve each unresolved dependency
		for depID, depOptions := range unresolved {
			feature, err := m.resolver.Resolve(ctx, depID, depOptions)
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

	for _, feature := range resolved {
		if feature.Metadata == nil {
			continue
		}

		// Check hard dependencies (dependsOn)
		for depID, depOptionsRaw := range feature.Metadata.DependsOn {
			if _, exists := resolved[depID]; !exists {
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
			if _, exists := resolved[depID]; !exists {
				// Soft dependencies don't have options specified
				if _, alreadyQueued := unresolved[depID]; !alreadyQueued {
					unresolved[depID] = make(map[string]interface{})
				}
			}
		}
	}

	return unresolved
}

// BuildDerivedImage builds a derived image with features installed.
// remoteUser is the configured remoteUser from devcontainer.json (can be empty to default to containerUser).
// containerUser is the container's user account (can be empty to default to "root").
func (m *Manager) BuildDerivedImage(ctx context.Context, baseImage, imageTag string, features []*Feature, buildDir, remoteUser, containerUser string) error {
	if len(features) == 0 {
		return nil
	}

	// Create build directory
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("failed to create build directory: %w", err)
	}

	// Generate Dockerfile
	generator := NewDockerfileGenerator(baseImage, features, buildDir, remoteUser, containerUser)
	dockerfile := generator.Generate()

	// Prepare build context
	if err := PrepareBuildContext(buildDir, features, dockerfile); err != nil {
		return fmt.Errorf("failed to prepare build context: %w", err)
	}

	// Build the image using docker CLI
	dockerfilePath := filepath.Join(buildDir, "Dockerfile.dcx-features")
	if err := buildImage(ctx, buildDir, dockerfilePath, imageTag); err != nil {
		return fmt.Errorf("failed to build derived image: %w", err)
	}

	return nil
}

// buildImage builds a Docker image using the CLI.
func buildImage(ctx context.Context, contextDir, dockerfilePath, tag string) error {
	// Build derived image with features. Docker layer cache is used for performance.
	// Cache is invalidated when config changes because the image tag includes configHash.
	args := []string{"build", "-t", tag, "-f", dockerfilePath, contextDir}

	cmd := execCommand(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// execCommand creates an exec.Cmd.
func execCommand(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// GetDerivedImageTag returns a deterministic tag for a derived image.
func GetDerivedImageTag(envKey, configHash string) string {
	return fmt.Sprintf("dcx/%s:%s-features", envKey, configHash[:12])
}

// HasFeatures returns true if the config has any features.
func HasFeatures(featuresConfig map[string]interface{}) bool {
	return len(featuresConfig) > 0
}
