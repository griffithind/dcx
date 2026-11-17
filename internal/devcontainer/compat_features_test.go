package devcontainer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Mounts merge image metadata, then features, then devcontainer.json, so a
// feature never replaces the user's mount on the same target (F10).
func TestUserMountsWinOverFeatureMounts(t *testing.T) {
	ws := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(ws, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	write(".devcontainer/devcontainer.json", `{
		"image": "alpine:3.24",
		"features": {"./f": {}},
		"mounts": [{"type": "volume", "source": "user-data", "target": "/data"}]
	}`)
	write(".devcontainer/f/devcontainer-feature.json", `{
		"id": "f",
		"mounts": [
			{"type": "volume", "source": "feature-data", "target": "/data"},
			{"type": "volume", "source": "feature-cache", "target": "/cache"}
		]
	}`)
	write(".devcontainer/f/install.sh", "#!/bin/sh\n")

	resolved, err := ResolveWorkspace(context.Background(), ResolveOptions{WorkspacePath: ws})
	require.NoError(t, err)
	sources := map[string]string{}
	for _, m := range resolved.Mounts {
		sources[m.Target] = m.Source
	}
	assert.Equal(t, "user-data", sources["/data"])
	assert.Equal(t, "feature-cache", sources["/cache"])
}
