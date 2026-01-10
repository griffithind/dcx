package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/docker/docker/client"
	"github.com/griffithind/dcx/internal/devcontainer"
)

// BuildFromDockerfile builds an image from a Dockerfile using Docker CLI.
// This uses `docker buildx build` to get BuildKit-style progress output.
func (b *CLIBuilder) BuildFromDockerfile(ctx context.Context, opts DockerfileBuildOptions) (string, error) {
	// Resolve context path
	contextPath := opts.Context
	if contextPath == "" {
		contextPath = "."
	}
	contextPath, err := filepath.Abs(contextPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve context path: %w", err)
	}

	// Build command arguments
	args := []string{"buildx", "build"}

	// Tag
	if opts.Tag != "" {
		args = append(args, "-t", opts.Tag)
	}

	// Dockerfile - resolve to absolute path if relative
	if opts.Dockerfile != "" {
		dockerfilePath := opts.Dockerfile
		if !filepath.IsAbs(dockerfilePath) {
			// If relative, make it relative to the context
			dockerfilePath = filepath.Join(contextPath, dockerfilePath)
		}
		args = append(args, "-f", dockerfilePath)
	}

	// Build args
	for k, v := range opts.Args {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	// Add metadata label if provided
	if opts.Metadata != "" {
		args = append(args, "--label", fmt.Sprintf("%s=%s", devcontainer.DevcontainerMetadataLabel, opts.Metadata))
	}

	// Cache from
	for _, cache := range opts.CacheFrom {
		args = append(args, "--cache-from", cache)
	}

	// Other flags
	if opts.NoCache {
		args = append(args, "--no-cache")
	}
	if opts.Pull {
		args = append(args, "--pull")
	}
	if opts.Target != "" {
		args = append(args, "--target", opts.Target)
	}

	// Add build contexts (for BuildKit builds with --build-context flag)
	for name, path := range opts.BuildContexts {
		args = append(args, "--build-context", fmt.Sprintf("%s=%s", name, path))
	}

	// Load the image into Docker (default for single-platform builds)
	args = append(args, "--load")

	// Context path
	args = append(args, contextPath)

	// Create and configure command
	cmd := exec.CommandContext(ctx, "docker", args...)

	// Set output - use provided progress writer or stdout/stderr
	if opts.Progress != nil {
		cmd.Stdout = opts.Progress
		cmd.Stderr = opts.Progress
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// Run the build
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docker build failed: %w", err)
	}

	return opts.Tag, nil
}

// ImageExists checks if an image exists locally.
func (b *CLIBuilder) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	_, _, err := b.client.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// PullImage pulls an image from a registry using Docker CLI.
func (b *CLIBuilder) PullImage(ctx context.Context, imageRef string, progress io.Writer) error {
	args := []string{"pull", imageRef}

	cmd := exec.CommandContext(ctx, "docker", args...)

	if progress != nil {
		cmd.Stdout = progress
		cmd.Stderr = progress
	} else {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}

	return nil
}

// GetImageID returns the ID of an image.
func (b *CLIBuilder) GetImageID(ctx context.Context, imageRef string) (string, error) {
	info, _, err := b.client.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image: %w", err)
	}
	return info.ID, nil
}

// GetImageLabels returns the labels for an image.
func (b *CLIBuilder) GetImageLabels(ctx context.Context, imageRef string) (map[string]string, error) {
	info, _, err := b.client.ImageInspectWithRaw(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect image: %w", err)
	}
	if info.Config == nil {
		return nil, nil
	}
	return info.Config.Labels, nil
}

// ResolveImage ensures an image is available locally, pulling if necessary.
func (b *CLIBuilder) ResolveImage(ctx context.Context, imageRef string, pull bool, progress io.Writer) error {
	exists, err := b.ImageExists(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("failed to check image: %w", err)
	}

	if !exists || pull {
		if progress != nil {
			fmt.Fprintf(progress, "Pulling image: %s\n", imageRef)
		}
		if err := b.PullImage(ctx, imageRef, progress); err != nil {
			// If pull fails and image exists locally, that's ok
			if exists {
				return nil
			}
			return fmt.Errorf("failed to pull image: %w", err)
		}
	}

	return nil
}
