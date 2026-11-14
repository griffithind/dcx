//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFeatureEntrypointAndHooksE2E guards a feature's entrypoint (ignored
// before, so features like docker-in-docker never started their daemon)
// and its lifecycle hooks, which now run from the resolved per-phase hook
// lists before devcontainer.json's.
func TestFeatureEntrypointAndHooksE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := t.TempDir()
	featureDir := filepath.Join(workspace, ".devcontainer", "features", "with-entrypoint")
	require.NoError(t, os.MkdirAll(featureDir, 0o755))
	write := func(path, content string, mode os.FileMode) {
		require.NoError(t, os.WriteFile(path, []byte(content), mode))
	}
	write(filepath.Join(featureDir, "devcontainer-feature.json"), `{
		"id": "with-entrypoint",
		"version": "1.0.0",
		"name": "With Entrypoint",
		"entrypoint": "/usr/local/share/with-entrypoint-init.sh",
		"postCreateCommand": "echo feature >> /tmp/post-create.log"
	}`, 0o644)
	write(filepath.Join(featureDir, "install.sh"), `#!/bin/sh
set -e
cat > /usr/local/share/with-entrypoint-init.sh <<'EOF'
#!/bin/sh
echo "ran with $# args" > /tmp/entrypoint-ran
exec "$@"
EOF
chmod +x /usr/local/share/with-entrypoint-init.sh
`, 0o755)
	write(filepath.Join(workspace, ".devcontainer", "devcontainer.json"), fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"features": {"./features/with-entrypoint": {}},
		"postCreateCommand": {"local": "echo local >> /tmp/post-create.log"}
	}`, helpers.UniqueTestName(t)), 0o644)
	t.Cleanup(func() { helpers.RunDCXInDir(t, workspace, "down") })

	helpers.RunDCXInDirSuccess(t, workspace, "up")

	stdout, stderr, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/entrypoint-ran")
	require.NoError(t, err, "the feature entrypoint did not run: stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, "ran with 0 args", strings.TrimSpace(stdout))

	stdout, stderr, err = helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", "/tmp/post-create.log")
	require.NoError(t, err, "stdout=%s stderr=%s", stdout, stderr)
	assert.Equal(t, "feature\nlocal", strings.TrimSpace(stdout), "feature hooks run before devcontainer.json's")
}
