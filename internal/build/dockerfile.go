package build

import (
	"context"
	"fmt"
	"path/filepath"
)

// BuildFromDockerfile builds an image from a Dockerfile through the
// engine. Relative context and Dockerfile paths are resolved first (the
// Dockerfile relative to the context), so the engine gets absolute paths.
func (b *Builder) BuildFromDockerfile(ctx context.Context, opts DockerfileBuildOptions) (string, error) {
	contextPath := opts.Context
	if contextPath == "" {
		contextPath = "."
	}
	contextPath, err := filepath.Abs(contextPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve context path: %w", err)
	}
	opts.Context = contextPath
	if opts.Dockerfile != "" && !filepath.IsAbs(opts.Dockerfile) {
		opts.Dockerfile = filepath.Join(contextPath, opts.Dockerfile)
	}
	if err := b.engine.BuildImage(ctx, opts); err != nil {
		return "", fmt.Errorf("docker build failed: %w", err)
	}
	return opts.Tag, nil
}
