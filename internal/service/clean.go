package service

import (
	"context"

	"github.com/griffithind/dcx/internal/container"
)

// CleanDerivedImages removes dcx's derived images (base, features and UID
// layers) that no container uses: every workspace's, or with currentOnly
// only this workspace's.
func (s *DevContainerService) CleanDerivedImages(ctx context.Context, currentOnly bool) (*container.CleanupResult, error) {
	scope := ""
	if currentOnly {
		scope = s.Identity().WorkspaceID
	}
	return s.engine.CleanupDerivedImages(ctx, scope)
}

// CleanDanglingImages removes dangling (untagged) images.
func (s *DevContainerService) CleanDanglingImages(ctx context.Context) (*container.CleanupResult, error) {
	return s.engine.CleanupDanglingImages(ctx)
}

// DerivedImageStats reports how many dcx images exist and their size.
func (s *DevContainerService) DerivedImageStats(ctx context.Context) (count int, size int64, err error) {
	return s.engine.DerivedImageStats(ctx)
}
