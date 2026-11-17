// Package build prepares and runs the image builds dcx adds on top of a
// base image: the features layer and the UID update layer. It generates
// Dockerfiles and build contexts; the builds themselves run through an
// Engine (the container engine), so this package never shells out.
package build

import (
	"context"
	"io"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/report"
)

// Engine is what builds need from the container engine. container.Engine
// satisfies it (this package cannot import container, which imports it).
type Engine interface {
	// BuildImage builds opts (paths already absolute) and tags it opts.Tag.
	BuildImage(ctx context.Context, opts DockerfileBuildOptions) error
	ImageExists(ctx context.Context, imageRef string) (bool, error)
	ImageLabels(ctx context.Context, imageRef string) (map[string]string, error)
	ImageConfig(ctx context.Context, imageRef string) (ImageConfig, error)
}

// ImageConfig is the part of an image's configuration dcx reads.
type ImageConfig struct {
	// User is the image's USER ("" means root).
	User       string
	Entrypoint []string
	Cmd        []string
}

// DockerfileBuildOptions contains options for building from a Dockerfile.
type DockerfileBuildOptions struct {
	// Tag is the image tag to apply.
	Tag string

	// ExtraTags are further tags (devcontainer.json's image, when it is
	// set together with build).
	ExtraTags []string

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

	// Secrets are BuildKit secrets to make available during the build.
	// Map of secret ID to temp file path containing the secret value.
	// These are passed as --secret id=<id>,src=<path> flags.
	Secrets map[string]string

	// Options are additional docker build CLI options from devcontainer.json build.options.
	// These are passed directly to the docker build command.
	Options []string
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

	// ServiceUser is the compose service's user, which features install
	// for when there is no containerUser.
	ServiceUser string

	// Rebuild forces rebuilding even if cached.
	Rebuild bool

	// NoCache builds without BuildKit's cache.
	NoCache bool

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

// Builder runs dcx's image builds through an Engine.
type Builder struct {
	engine Engine
	report report.Reporter
}

// NewBuilder creates a builder that runs builds on engine and reports
// progress to rep.
func NewBuilder(engine Engine, rep report.Reporter) *Builder {
	return &Builder{engine: engine, report: report.OrDiscard(rep)}
}
