// Package build provides build-time operations for devcontainer images.
// This package uses Docker CLI commands for builds to get native BuildKit output.
package build

import (
	"context"
	"io"

	"github.com/docker/docker/client"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
)

// ImageBuilder defines the interface for building container images.
// This abstraction allows for testing and alternative implementations.
type ImageBuilder interface {
	// BuildFromDockerfile builds an image from a Dockerfile.
	BuildFromDockerfile(ctx context.Context, opts DockerfileBuildOptions) (string, error)

	// BuildWithFeatures builds a derived image with features installed.
	BuildWithFeatures(ctx context.Context, opts FeatureBuildOptions) (string, error)

	// BuildUIDUpdate builds an image with updated UID/GID for the remote user.
	BuildUIDUpdate(ctx context.Context, opts UIDBuildOptions) (string, error)

	// ImageExists checks if an image exists locally.
	ImageExists(ctx context.Context, imageRef string) (bool, error)

	// PullImage pulls an image from a registry.
	PullImage(ctx context.Context, imageRef string, progress io.Writer) error
}

// DockerfileBuildOptions contains options for building from a Dockerfile.
type DockerfileBuildOptions struct {
	// Tag is the image tag to apply.
	Tag string

	// Dockerfile is the path to the Dockerfile.
	Dockerfile string

	// Context is the build context directory.
	Context string

	// Args are build-time arguments.
	Args map[string]string

	// Target is the target build stage.
	Target string

	// CacheFrom is a list of images to use as cache sources.
	CacheFrom []string

	// NoCache disables build cache.
	NoCache bool

	// Pull forces pulling the base image.
	Pull bool

	// Progress is the writer for build output.
	Progress io.Writer

	// Metadata is the devcontainer.metadata label value to embed in the image.
	// If empty, no metadata label is added.
	Metadata string

	// BuildContexts are additional named build contexts (--build-context flag).
	// Used for BuildKit builds to pass feature content without copying to main context.
	// Map of context name to filesystem path.
	BuildContexts map[string]string
}

// FeatureBuildOptions contains options for building with features.
type FeatureBuildOptions struct {
	// BaseImage is the image to build on top of.
	BaseImage string

	// Tag is the tag for the resulting image.
	Tag string

	// Features is the list of features to install.
	Features []*features.Feature

	// RemoteUser is the configured remoteUser from devcontainer.json.
	RemoteUser string

	// ContainerUser is the container's user account.
	ContainerUser string

	// Rebuild forces rebuilding even if cached.
	Rebuild bool

	// Progress is the writer for build output.
	Progress io.Writer

	// BaseImageMetadata is the devcontainer.metadata label from the base image.
	// This will be merged with feature metadata in the final image.
	BaseImageMetadata string

	// LocalConfig is the local devcontainer.json config for metadata merging.
	LocalConfig *devcontainer.DevContainerConfig
}

// UIDBuildOptions contains options for UID update builds.
type UIDBuildOptions struct {
	// BaseImage is the image to build on top of.
	BaseImage string

	// Tag is the tag for the resulting image.
	Tag string

	// RemoteUser is the user whose UID/GID should be updated.
	RemoteUser string

	// ImageUser is the user the container should run as after UID update.
	ImageUser string

	// HostUID is the host user's UID to match.
	HostUID int

	// HostGID is the host user's GID to match.
	HostGID int

	// Rebuild forces rebuilding even if cached.
	Rebuild bool

	// Progress is the writer for build output.
	Progress io.Writer

	// Metadata is the devcontainer.metadata label value to preserve.
	// The UID layer should preserve metadata from the base image.
	Metadata string
}

// CLIBuilder implements ImageBuilder using Docker CLI for builds and SDK for inspection.
// The name reflects that all build operations use the Docker CLI (docker buildx build)
// while the SDK is only used for image inspection operations.
type CLIBuilder struct {
	client *client.Client
}

// NewCLIBuilder creates a new image builder.
func NewCLIBuilder(cli *client.Client) *CLIBuilder {
	return &CLIBuilder{client: cli}
}

// Client returns the underlying Docker client.
func (b *CLIBuilder) Client() *client.Client {
	return b.client
}

// Close closes the Docker client.
func (b *CLIBuilder) Close() error {
	return b.client.Close()
}

// Ensure CLIBuilder implements ImageBuilder.
var _ ImageBuilder = (*CLIBuilder)(nil)
