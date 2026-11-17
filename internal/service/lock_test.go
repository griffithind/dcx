package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lockfile"
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

// An empty lockfile is the spec's "create it" marker: up and build write
// it from the features they resolved (F14); without the marker they never
// write one.
func TestInitLockfileFillsTheEmptyMarker(t *testing.T) {
	empty := ""
	svc := lockTestService(t, &empty)
	configPath := filepath.Join(svc.workspacePath, ".devcontainer", "devcontainer.json")
	resolved := &devcontainer.ResolvedDevContainer{
		ConfigPath: configPath,
		Features: []*features.Feature{{
			ID:             "ghcr.io/devcontainers/features/go:1",
			Source:         features.FeatureSource{Type: features.SourceTypeOCI, Registry: "ghcr.io", Repository: "devcontainers/features", Resource: "go", Version: "1"},
			ManifestDigest: "sha256:m",
			Metadata:       &features.FeatureMetadata{ID: "go", Version: "1.3.0"},
		}},
	}
	require.NoError(t, svc.initLockfile(resolved))
	lf, initMarker, err := lockfile.Load(configPath)
	require.NoError(t, err)
	assert.False(t, initMarker)
	assert.Equal(t, lockfile.LockedFeature{Version: "1.3.0", Resolved: "ghcr.io/devcontainers/features/go@sha256:m", Integrity: "sha256:m"},
		lf.Features["ghcr.io/devcontainers/features/go:1"])

	missing := lockTestService(t, nil)
	resolved.ConfigPath = filepath.Join(missing.workspacePath, ".devcontainer", "devcontainer.json")
	require.NoError(t, missing.initLockfile(resolved))
	assert.NoFileExists(t, lockfile.Path(resolved.ConfigPath))
}
