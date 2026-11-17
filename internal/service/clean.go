package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/dcxhome"
	"github.com/griffithind/dcx/internal/state"
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

// WorkspacePrune is what PruneWorkspaces removed (or, as a dry run, would
// remove).
type WorkspacePrune struct {
	// Folders are workspace folders in $DCX_HOME/workspaces.
	Folders int
	// Legacy are files older dcx versions kept per workspace (hostkeys/,
	// locks/, compose/).
	Legacy int
	// Busy are folders skipped because a dcx command holds their lock.
	Busy int
}

// PruneWorkspaces removes what dcx keeps on the host for workspaces that
// have no container (`dcx down` was run, the container was removed outside
// dcx, or the workspace folder was deleted): their folders in
// $DCX_HOME/workspaces, and the files older dcx versions kept in
// hostkeys/, locks/ and compose/. A workspace whose lock is held (an `up`
// in progress) is kept. A pruned workspace gets a new SSH host key on its
// next `dcx up`, which updates the known_hosts pin.
func (s *DevContainerService) PruneWorkspaces(ctx context.Context, dryRun bool) (*WorkspacePrune, error) {
	containers, err := s.engine.ListContainersWithLabels(ctx, map[string]string{state.LabelManaged: "true"})
	if err != nil {
		return nil, err
	}
	live := map[string]bool{}
	for _, c := range containers {
		live[c.Labels[state.LabelWorkspaceID]] = true
	}

	var res WorkspacePrune
	ids, err := dcxhome.WorkspaceIDs()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if live[id] {
			continue
		}
		if dryRun {
			res.Folders++
			continue
		}
		unlock, ok, err := tryLockWorkspace(id)
		if err != nil {
			return nil, err
		}
		if !ok {
			res.Busy++
			continue
		}
		dir, err := dcxhome.WorkspaceDir(id)
		if err == nil {
			err = os.RemoveAll(dir)
		}
		unlock()
		if err != nil {
			return nil, fmt.Errorf("remove workspace folder: %w", err)
		}
		res.Folders++
	}

	// Older layouts: hostkeys/<id>.key and compose/<id>/ of workspaces
	// without a container, and every locks/<id>.lock (no dcx uses them).
	legacy := []struct {
		dir   string
		id    func(name string) string
		alive bool
	}{
		{dcxhome.LegacyDirs.HostKeys, func(n string) string { return strings.TrimSuffix(n, ".key") }, true},
		{dcxhome.LegacyDirs.Compose, func(n string) string { return n }, true},
		{dcxhome.LegacyDirs.Locks, nil, false},
	}
	for _, l := range legacy {
		dir, err := dcxhome.Path(l.dir)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		removed := 0
		for _, e := range entries {
			if l.alive && live[l.id(e.Name())] {
				continue
			}
			if !dryRun {
				if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
					return nil, err
				}
			}
			removed++
		}
		res.Legacy += removed
		if !dryRun && removed == len(entries) {
			_ = os.Remove(dir)
		}
	}
	return &res, nil
}
