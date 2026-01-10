package build

import (
	"context"
	"fmt"
	"os"

	"github.com/griffithind/dcx/internal/features"
)

// BuildWithFeatures builds a derived image with features installed.
func (b *SDKBuilder) BuildWithFeatures(ctx context.Context, opts FeatureBuildOptions) (string, error) {
	if len(opts.Features) == 0 {
		// No features to install, return base image
		return opts.BaseImage, nil
	}

	// Check if derived image already exists and is up-to-date
	if !opts.Rebuild {
		exists, err := b.ImageExists(ctx, opts.Tag)
		if err == nil && exists {
			fmt.Printf("Using cached derived image\n")
			return opts.Tag, nil
		}
	}

	// Print header with feature list
	fmt.Printf("Building derived image with %d feature(s)\n", len(opts.Features))
	for i, f := range opts.Features {
		name := f.ID
		if f.Metadata != nil && f.Metadata.Name != "" {
			name = f.Metadata.Name
		}
		fmt.Printf(" => %d. %s\n", i+1, name)
	}

	// Create temporary build directory
	tempBuildDir, err := os.MkdirTemp("", "dcx-build-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp build directory: %w", err)
	}
	defer os.RemoveAll(tempBuildDir)

	// Resolve user settings
	containerUser := opts.ContainerUser
	if containerUser == "" {
		containerUser = "root"
	}
	remoteUser := opts.RemoteUser
	if remoteUser == "" {
		remoteUser = containerUser
	}

	// Generate Dockerfile using the features package
	generator := features.NewDockerfileGenerator(opts.BaseImage, opts.Features, tempBuildDir, remoteUser, containerUser)
	dockerfile := generator.Generate()

	// Prepare build context with feature files
	if err := features.PrepareBuildContext(tempBuildDir, opts.Features, dockerfile); err != nil {
		return "", fmt.Errorf("failed to prepare build context: %w", err)
	}

	// Build the image using Docker CLI
	_, err = b.BuildFromDockerfile(ctx, DockerfileBuildOptions{
		Tag:        opts.Tag,
		Dockerfile: "Dockerfile.dcx-features",
		Context:    tempBuildDir,
	})
	if err != nil {
		return "", fmt.Errorf("failed to build derived image: %w", err)
	}

	return opts.Tag, nil
}

// BuildDerivedImage is a convenience function that builds a derived image with features.
// This can be called without creating an SDKBuilder instance.
func BuildDerivedImage(ctx context.Context, baseImage, imageTag string, feats []*features.Feature, remoteUser, containerUser string) error {
	builder, err := NewSDKBuilderFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create builder: %w", err)
	}
	defer builder.Close()

	_, err = builder.BuildWithFeatures(ctx, FeatureBuildOptions{
		BaseImage:     baseImage,
		Tag:           imageTag,
		Features:      feats,
		RemoteUser:    remoteUser,
		ContainerUser: containerUser,
	})
	return err
}
