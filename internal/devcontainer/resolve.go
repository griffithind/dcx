package devcontainer

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/griffithind/dcx/internal/lockfile"
	"github.com/griffithind/dcx/internal/report"
)

// ImageMetadataSource reads an image's devcontainer.metadata label. The
// service implements it with the container engine (pulling the image when
// asked to); resolution never talks to Docker itself.
type ImageMetadataSource interface {
	// ImageMetadata returns the image's devcontainer.metadata label, ""
	// when it has none, or an error when the image is not available.
	ImageMetadata(ctx context.Context, imageRef string) (string, error)
}

// ResolveOptions are the inputs of ResolveWorkspace.
type ResolveOptions struct {
	// WorkspacePath is the absolute workspace root.
	WorkspacePath string
	// ConfigPath selects devcontainer.json ("" auto-detects it).
	ConfigPath string
	// Images supplies base image metadata; nil resolves without it.
	Images ImageMetadataSource
	// Lockfile pins feature versions: LockfileUse loads
	// devcontainer-lock.json next to the config, LockfileIgnore does not.
	Lockfile LockfileMode
	// ForcePull re-fetches features from their registries.
	ForcePull bool
	// Report receives progress and warnings (nil discards them).
	Report report.Reporter
}

// LockfileMode says whether resolution pins features with the lockfile.
type LockfileMode int

const (
	// LockfileIgnore resolves features from their references alone.
	LockfileIgnore LockfileMode = iota
	// LockfileUse pins features to devcontainer-lock.json when present.
	LockfileUse
)

// ResolveWorkspace is the one resolution pipeline: it loads and substitutes
// devcontainer.json, merges the base image's devcontainer.metadata, applies
// the lockfile, resolves features, computes the identity and the
// staleness key, and returns the result. `dcx up`, status, build, lock,
// debug and the golden tests all resolve through it.
func ResolveWorkspace(ctx context.Context, opts ResolveOptions) (*ResolvedDevContainer, error) {
	rep := report.OrDiscard(opts.Report)
	local, configPath, err := Load(opts.WorkspacePath, opts.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}
	// Ambiguous configs (image and dockerComposeFile, …) used to resolve
	// silently by an internal precedence.
	if err := Validate(local); err != nil {
		return nil, fmt.Errorf("invalid configuration %s: %w", configPath, err)
	}
	metaImage := metadataImage(local, configPath)
	metaLabel, imageConfigs := imageMetadata(ctx, metaImage, opts.Images, rep)
	// Image metadata gets the same substitution as devcontainer.json: a
	// prebuilt image's feature mounts name volumes with ${devcontainerId}.
	subCtx := NewSubstitutionContext(opts.WorkspacePath, configPath, local)
	for i := range imageConfigs {
		SubstituteConfig(&imageConfigs[i], subCtx)
	}
	cfg := MergeMetadata(local, imageConfigs)

	var lf *lockfile.Lockfile
	if opts.Lockfile == LockfileUse && len(cfg.Features) > 0 {
		if lf, _, err = lockfile.Load(configPath); err != nil {
			rep.Warnf("Ignoring lockfile: %v", err)
			lf = nil
		}
	}

	// The staleness key is computed from the file as written.
	raw, err := ParseFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	resolved, err := NewBuilder(slog.Default(), rep).Build(ctx, BuilderOptions{
		Raw:           raw,
		ConfigPath:    configPath,
		WorkspaceRoot: opts.WorkspacePath,
		Config:        cfg,
		Local:         local,
		ImageMetadata: imageConfigs,
		MetadataImage: metaImage,
		MetadataLabel: metaLabel,
		Lockfile:      lf,
		ForcePull:     opts.ForcePull,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to resolve devcontainer: %w", err)
	}
	return resolved, nil
}

// imageMetadata returns the devcontainer.metadata label of image (see
// metadataImage: the image, or the base image a Dockerfile or compose
// service builds FROM) and its parsed entries.
func imageMetadata(ctx context.Context, image string, images ImageMetadataSource, rep report.Reporter) (string, []DevContainerConfig) {
	if images == nil || image == "" {
		return "", nil
	}
	label, err := images.ImageMetadata(ctx, image)
	if err != nil || label == "" {
		// Not available locally (and not pulled): resolve without it.
		return "", nil
	}
	imageConfigs, err := ParseImageMetadata(label)
	if err != nil {
		rep.Warnf("Ignoring invalid devcontainer.metadata of %s: %v", image, err)
		return "", nil
	}
	return label, imageConfigs
}
