//go:build e2e

package e2e

import (
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserEnvProbeE2E: lifecycle hooks get the environment the
// userEnvProbe captured.
func TestUserEnvProbeE2E(t *testing.T) {
	t.Parallel()
	stdout, _, err := helpers.RunDCXInDir(t, debianEnv(t).Dir, "ssh", "--", "cat", "/tmp/probe-result.txt")
	require.NoError(t, err)
	assert.Contains(t, stdout, "PATH=", "probed env should contain PATH")
	assert.Contains(t, stdout, "HOME=", "probed env should contain HOME")
}

// TestEtcProfilePatchingE2E: /etc/profile is patched once to keep PATH.
func TestEtcProfilePatchingE2E(t *testing.T) {
	t.Parallel()
	stdout, _, err := helpers.RunDCXInDir(t, debianEnv(t).Dir, "ssh", "--", "sh", "-c", "test -f /var/lib/dcx/.patchEtcProfileMarker && echo patched || echo not-patched")
	require.NoError(t, err)
	assert.Equal(t, "patched", strings.TrimSpace(stdout), "/etc/profile should be patched")
}
