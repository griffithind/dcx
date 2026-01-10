// Package build provides build-time operations for devcontainer images.
// This package uses Docker CLI commands for builds to get native BuildKit output.
package build

import (
	"context"
	"io"

	"github.com/docker/docker/client"
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

	// Progress is the writer for build output (unused with CLI, kept for interface compatibility).
	Progress io.Writer
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

	// Progress is the writer for build output (unused with CLI, kept for interface compatibility).
	Progress io.Writer
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

	// Progress is the writer for build output (unused with CLI, kept for interface compatibility).
	Progress io.Writer
}

// SDKBuilder implements ImageBuilder using Docker CLI for builds and SDK for inspection.
type SDKBuilder struct {
	client *client.Client
}

// NewSDKBuilder creates a new image builder.
func NewSDKBuilder(cli *client.Client) *SDKBuilder {
	return &SDKBuilder{client: cli}
}

// NewSDKBuilderFromEnv creates a new image builder from environment.
func NewSDKBuilderFromEnv() (*SDKBuilder, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}
	return &SDKBuilder{client: cli}, nil
}

// Client returns the underlying Docker client.
func (b *SDKBuilder) Client() *client.Client {
	return b.client
}

// Close closes the Docker client.
func (b *SDKBuilder) Close() error {
	return b.client.Close()
}

// Ensure SDKBuilder implements ImageBuilder.
var _ ImageBuilder = (*SDKBuilder)(nil)
