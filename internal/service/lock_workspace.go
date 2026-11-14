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

// workspaceLockDir holds one lock file per workspace (~/.dcx/locks).
func workspaceLockDir() (string, error) {
	return dcxhome.Path("locks")
}

// lockWorkspace takes the workspace's exclusive lock, so two `dcx up` runs
// (two terminals, an editor and a terminal) cannot create, recreate or
// set up the same environment at once. It waits for a holder, telling the
// user, until ctx is cancelled. The lock goes away with the process.
func (s *DevContainerService) lockWorkspace(ctx context.Context, workspaceID string) (unlock func(), err error) {
	dir, err := workspaceLockDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create lock dir: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(dir, workspaceID+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open workspace lock: %w", err)
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
