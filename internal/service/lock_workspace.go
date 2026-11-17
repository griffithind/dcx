package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/griffithind/dcx/internal/dcxhome"
)

// lockWorkspace takes the workspace's exclusive lock, so two `dcx up` runs
// (two terminals, an editor and a terminal) cannot create, recreate or
// set up the same environment at once. It waits for a holder, telling the
// user, until ctx is cancelled. The lock goes away with the process.
func (s *DevContainerService) lockWorkspace(ctx context.Context, workspaceID string) (unlock func(), err error) {
	f, err := openWorkspaceLock(workspaceID)
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd())
	waiting := false
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(fd, syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			_ = f.Close()
			return nil, fmt.Errorf("lock workspace: %w", err)
		}
		if !waiting {
			s.report.Infof("Waiting for another dcx command on this workspace to finish...")
			waiting = true
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// openWorkspaceLock opens (creating) the lock file in the workspace's
// folder.
func openWorkspaceLock(workspaceID string) (*os.File, error) {
	path, err := dcxhome.WorkspacePath(workspaceID, dcxhome.LockFile)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create workspace folder: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open workspace lock: %w", err)
	}
	return f, nil
}

// tryLockWorkspace takes the workspace's lock if no one holds it (ok
// false otherwise), without waiting.
func tryLockWorkspace(workspaceID string) (unlock func(), ok bool, err error) {
	f, err := openWorkspaceLock(workspaceID)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("lock workspace: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true, nil
}
