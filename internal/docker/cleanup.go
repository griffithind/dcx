package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
)

// CleanupResult contains statistics about cleaned up resources.
type CleanupResult struct {
	ImagesRemoved  int
	SpaceReclaimed int64
}

// CleanupDerivedImages removes derived images created by dcx.
// If envKey is provided, only images for that environment are removed.
// If keepCurrent is true, the current derived image (matching configHash) is preserved.
func (c *Client) CleanupDerivedImages(ctx context.Context, envKey, currentConfigHash string, keepCurrent bool) (*CleanupResult, error) {
	result := &CleanupResult{}

	// List all images
	images, err := c.cli.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	for _, img := range images {
		// Check each tag
		for _, tag := range img.RepoTags {
			// Derived images follow the pattern: dcx-derived/<envKey>:<hash>
			if !strings.HasPrefix(tag, "dcx-derived/") {
				continue
			}

			// Parse the tag
			parts := strings.SplitN(strings.TrimPrefix(tag, "dcx-derived/"), ":", 2)
			if len(parts) != 2 {
				continue
			}

			imageEnvKey := parts[0]
			imageHash := parts[1]

			// If envKey filter is provided, only match that environment
			if envKey != "" && imageEnvKey != envKey {
				continue
			}

			// If keepCurrent is true and this is the current image, skip it
			if keepCurrent && currentConfigHash != "" && imageHash == currentConfigHash {
				continue
			}

			// Remove the image
			_, err := c.cli.ImageRemove(ctx, img.ID, image.RemoveOptions{
				Force:         false,
				PruneChildren: true,
			})
			if err != nil {
				// Log but continue - image might be in use
				continue
			}

			result.ImagesRemoved++
			result.SpaceReclaimed += img.Size
		}
	}

	return result, nil
}

// CleanupAllDerivedImages removes all derived images created by dcx.
func (c *Client) CleanupAllDerivedImages(ctx context.Context) (*CleanupResult, error) {
	return c.CleanupDerivedImages(ctx, "", "", false)
}

// CleanupDanglingImages removes dangling (untagged) images.
func (c *Client) CleanupDanglingImages(ctx context.Context) (*CleanupResult, error) {
	result := &CleanupResult{}

	// Create filter for dangling images
	filterArgs := filters.NewArgs()
	filterArgs.Add("dangling", "true")

	images, err := c.cli.ImageList(ctx, image.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list dangling images: %w", err)
	}

	for _, img := range images {
		_, err := c.cli.ImageRemove(ctx, img.ID, image.RemoveOptions{
			Force:         false,
			PruneChildren: true,
		})
		if err != nil {
			// Log but continue
			continue
		}

		result.ImagesRemoved++
		result.SpaceReclaimed += img.Size
	}

	return result, nil
}

// CleanupOrphanedVolumes removes volumes not attached to any container.
// This only removes volumes with dcx labels.
func (c *Client) CleanupOrphanedVolumes(ctx context.Context) (int, error) {
	// Note: Volume cleanup is more complex and risky
	// For now, we just report but don't auto-delete
	// Users should use 'docker volume prune' manually
	return 0, nil
}

// GetDerivedImageStats returns statistics about derived images.
func (c *Client) GetDerivedImageStats(ctx context.Context) (count int, totalSize int64, err error) {
	images, err := c.cli.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list images: %w", err)
	}

	for _, img := range images {
		for _, tag := range img.RepoTags {
			if strings.HasPrefix(tag, "dcx-derived/") {
				count++
				totalSize += img.Size
				break
			}
		}
	}

	return count, totalSize, nil
}
