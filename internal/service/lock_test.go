package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lockTestService(t *testing.T, lockfileContent *string) *DevContainerService {
	t.Helper()
	svc, _ := newTestService(t, &containertest.Fake{})
	dir := filepath.Join(svc.workspacePath, ".devcontainer")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "devcontainer.json"),
		[]byte(`{"name": "lock-test", "image": "alpine:latest", "features": {"ghcr.io/devcontainers/features/go:1": {}}}`), 0o644))
	if lockfileContent != nil {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "devcontainer-lock.json"), []byte(*lockfileContent), 0o644))
	}
	return svc
}

// Verifying without a lockfile used to report "verified": there was nothing
// to compare against, so no mismatches were found.
func TestLockVerifyAndFrozenRequireALockfile(t *testing.T) {
	empty := ""
	for _, mode := range []LockMode{LockModeVerify, LockModeFrozen} {
		_, err := lockTestService(t, nil).Lock(context.Background(), LockOptions{Mode: mode})
		assert.ErrorContains(t, err, "lockfile not found", "mode %v without a lockfile", mode)

		_, err = lockTestService(t, &empty).Lock(context.Background(), LockOptions{Mode: mode})
		assert.ErrorContains(t, err, "is empty", "mode %v with an init-marker lockfile", mode)
	}
}
