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

// ResolveAll resolves all features from a devcontainer.json features map.
func (m *Manager) ResolveAll(ctx context.Context, featuresConfig map[string]interface{}, overrideOrder []string) ([]*Feature, error) {
	if len(featuresConfig) == 0 {
		return nil, nil
	}

	// Resolve each feature
	features := make([]*Feature, 0, len(featuresConfig))

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

		features = append(features, feature)
	}

	// Validate dependencies
	if err := ValidateDependencies(features); err != nil {
		return nil, err
	}

	// Order features
	ordered, err := OrderFeatures(features, overrideOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to order features: %w", err)
	}

	return ordered, nil
}

// BuildDerivedImage builds a derived image with features installed.
func (m *Manager) BuildDerivedImage(ctx context.Context, baseImage, imageTag string, features []*Feature, buildDir string, verbose bool) error {
	if len(features) == 0 {
		return nil
	}

	// Create build directory
	if err := os.MkdirAll(buildDir, 0755); err != nil {
		return fmt.Errorf("failed to create build directory: %w", err)
	}

	// Generate Dockerfile
	generator := NewDockerfileGenerator(baseImage, features, buildDir)
	dockerfile := generator.Generate()

	// Prepare build context
	if err := PrepareBuildContext(buildDir, features, dockerfile); err != nil {
		return fmt.Errorf("failed to prepare build context: %w", err)
	}

	// Build the image using docker CLI
	dockerfilePath := filepath.Join(buildDir, "Dockerfile.dcx-features")
	if err := buildImage(ctx, buildDir, dockerfilePath, imageTag, verbose); err != nil {
		return fmt.Errorf("failed to build derived image: %w", err)
	}

	return nil
}

// buildImage builds a Docker image using the CLI.
func buildImage(ctx context.Context, contextDir, dockerfilePath, tag string, verbose bool) error {
	// Use --no-cache to ensure features are always installed fresh
	// This is necessary because the base image may have been rebuilt
	args := []string{"build", "--no-cache", "-t", tag, "-f", dockerfilePath, contextDir}

	cmd := execCommand(ctx, "docker", args...)

	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

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
