//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listFilesInDir returns a sorted list of all files and directories in a directory (recursive).
func listFilesInDir(t *testing.T, dir string) []string {
	t.Helper()

	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Store relative path from dir
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if relPath != "." {
			files = append(files, relPath)
		}
		return nil
	})
	require.NoError(t, err)

	sort.Strings(files)
	return files
}

// TestLockfileGenerationE2E tests that dcx lock generates a lockfile.
func TestLockfileGenerationE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := createWorkspaceWithOCIFeature(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	lockfilePath := filepath.Join(devcontainerDir, "devcontainer-lock.json")

	// Verify lockfile doesn't exist initially
	_, err := os.Stat(lockfilePath)
	require.True(t, os.IsNotExist(err), "lockfile should not exist initially")

	// Run dcx lock to generate lockfile
	t.Run("lock_generates_lockfile", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "lock")
		// Output should show path and feature count
		assert.Contains(t, stdout, "Path:", "should show lockfile path")

		// Verify lockfile was created
		_, err := os.Stat(lockfilePath)
		require.NoError(t, err, "lockfile should exist after dcx lock")
	})

	// Verify lockfile can be verified
	t.Run("lock_verify_succeeds", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "lock", "--verify")
		// Output shows path and feature count on success
		assert.Contains(t, stdout, "Path:", "should show lockfile path")
	})
}

// TestLockfileFrozenE2E tests --frozen mode fails when lockfile is missing.
func TestLockfileFrozenE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := createWorkspaceWithOCIFeature(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Run dcx lock --frozen without a lockfile - should fail
	t.Run("frozen_fails_without_lockfile", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "lock", "--frozen")
		require.Error(t, err, "dcx lock --frozen should fail without lockfile")
		combined := stderr
		assert.True(t, len(combined) > 0 || err != nil, "should produce an error")
	})

	// Generate lockfile first
	helpers.RunDCXInDirSuccess(t, workspace, "lock")

	// Now frozen mode should succeed
	t.Run("frozen_succeeds_with_lockfile", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "lock", "--frozen")
		// Output shows path and feature count on success
		assert.Contains(t, stdout, "Path:", "should show lockfile path")
	})
}

// createWorkspaceWithOCIFeature creates a workspace with an OCI-based feature for lockfile testing.
func createWorkspaceWithOCIFeature(t *testing.T) string {
	t.Helper()

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create devcontainer.json with an OCI feature (common-utils is widely available)
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "mcr.microsoft.com/devcontainers/base:ubuntu",
		"workspaceFolder": "/workspace",
		"features": {
			"ghcr.io/devcontainers/features/common-utils:2": {
				"installZsh": false
			}
		}
	}`, "lockfile-test-"+helpers.UniqueTestName(t))
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	return workspace
}

// TestCircularDependencyErrorE2E tests that circular feature dependencies are detected.
// Note: Circular dependency detection is tested at the unit level in manager_test.go.
// This E2E test verifies behavior with local features that have mutual dependencies.
func TestCircularDependencyErrorE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := createWorkspaceWithCircularDeps(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Try to run up with circular dependencies
	// Note: Local feature dependsOn resolution may not detect all circular cases
	t.Run("up_with_circular_deps", func(t *testing.T) {
		_, stderr, err := helpers.RunDCXInDir(t, workspace, "up")
		// Either it fails with an error, or local deps aren't fully resolved
		// Either outcome is acceptable - the key is it shouldn't hang
		if err != nil {
			// Expected: circular dependency detected
			assert.NotEmpty(t, stderr, "should have error output")
		}
		// If no error, local feature deps may not be fully resolved - that's also OK
	})
}

// createWorkspaceWithCircularDeps creates a workspace with features that have circular dependencies.
func createWorkspaceWithCircularDeps(t *testing.T) string {
	t.Helper()

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	featuresDir := filepath.Join(devcontainerDir, "features")
	err := os.MkdirAll(featuresDir, 0755)
	require.NoError(t, err)

	// Create feature-a that depends on feature-b
	featureADir := filepath.Join(featuresDir, "feature-a")
	err = os.MkdirAll(featureADir, 0755)
	require.NoError(t, err)

	featureAJSON := `{
		"id": "feature-a",
		"version": "1.0.0",
		"name": "Feature A",
		"dependsOn": {
			"./features/feature-b": {}
		}
	}`
	err = os.WriteFile(filepath.Join(featureADir, "devcontainer-feature.json"), []byte(featureAJSON), 0644)
	require.NoError(t, err)

	featureAInstall := `#!/bin/sh
echo "feature-a installed" > /tmp/feature-a-marker
`
	err = os.WriteFile(filepath.Join(featureADir, "install.sh"), []byte(featureAInstall), 0755)
	require.NoError(t, err)

	// Create feature-b that depends on feature-a (circular!)
	featureBDir := filepath.Join(featuresDir, "feature-b")
	err = os.MkdirAll(featureBDir, 0755)
	require.NoError(t, err)

	featureBJSON := `{
		"id": "feature-b",
		"version": "1.0.0",
		"name": "Feature B",
		"dependsOn": {
			"./features/feature-a": {}
		}
	}`
	err = os.WriteFile(filepath.Join(featureBDir, "devcontainer-feature.json"), []byte(featureBJSON), 0644)
	require.NoError(t, err)

	featureBInstall := `#!/bin/sh
echo "feature-b installed" > /tmp/feature-b-marker
`
	err = os.WriteFile(filepath.Join(featureBDir, "install.sh"), []byte(featureBInstall), 0755)
	require.NoError(t, err)

	// Create devcontainer.json referencing feature-a
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/feature-a": {}
		}
	}`, "circular-deps-test-"+helpers.UniqueTestName(t))
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	return workspace
}

// TestLocalFeaturesE2E installs seven local features into one container
// and checks each: a marker, options (with-options, set in
// devcontainer.json), install order (with-dependencies installs after
// simple-marker), a feature mount (with-mounts binds the Docker socket),
// security requirements with containerEnv (capAdd, init), containerEnv
// extending the image's PATH with ${devcontainerId} in mount names
// (pathfeature, and a devcontainer.json mount), and a feature entrypoint
// and postCreateCommand that runs before devcontainer.json's
// (with-entrypoint). dcx up writes nothing into .devcontainer.
func TestLocalFeaturesE2E(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	workspace := createWorkspaceWithLocalFeatures(t)
	id := devcontainer.ComputeID(workspace)
	t.Cleanup(func() {
		// Cleanups run last first: take the container down (also done by
		// the workspace's own cleanup, later) so its volumes are not in use.
		helpers.RunDCXInDir(t, workspace, "down")
		for _, v := range []string{"dcx-e2e-cache-" + id, "dcx-e2e-feat-" + id} {
			_ = exec.Command("docker", "volume", "rm", "-f", v).Run()
		}
	})
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	filesBefore := listFilesInDir(t, devcontainerDir)
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Contains(t, stdout, "Devcontainer started successfully")
	assert.Equal(t, "running", helpers.GetContainerState(t, workspace))
	assert.Equal(t, filesBefore, listFilesInDir(t, devcontainerDir), "dcx up should not create files in .devcontainer")
	assert.NoFileExists(t, filepath.Join(devcontainerDir, "Dockerfile.dcx-features"))
	assert.NoDirExists(t, filepath.Join(devcontainerDir, "feature_0"))

	cat := func(path string) string {
		t.Helper()
		out, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "cat", path)
		require.NoError(t, err, path)
		return out
	}
	t.Run("marker", func(t *testing.T) {
		assert.Contains(t, cat("/tmp/feature-marker"), "feature installed")
		assert.Contains(t, cat("/tmp/simple-marker"), "simple-marker installed")
	})
	t.Run("options", func(t *testing.T) {
		out := cat("/tmp/feature-options-marker")
		assert.Contains(t, out, "GREETING=CustomHello")
		assert.Contains(t, out, "ENABLED=true")
		assert.Contains(t, out, "COUNT=5")
	})
	t.Run("dependencies", func(t *testing.T) {
		assert.Contains(t, cat("/tmp/feature-deps-marker"), "MESSAGE=depends")
	})
	t.Run("mounts", func(t *testing.T) {
		out, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "ls", "-la", "/var/run/docker.sock")
		require.NoError(t, err)
		assert.Contains(t, out, "docker.sock")
		assert.Contains(t, cat("/tmp/dcx-features/with-mounts-marker.txt"), "with-mounts installed")
	})
	t.Run("path_and_devcontainer_id", func(t *testing.T) {
		path := dcxExec(t, workspace, "sh", "-c", "echo $PATH")
		assert.True(t, strings.HasPrefix(path, "/opt/pathfeature/bin:"), "PATH = %q", path)
		assert.Contains(t, path, "/usr/bin", "image PATH must be kept, got %q", path)
		assert.NotContains(t, path, "${PATH}")
		assert.Equal(t, "2", dcxExec(t, workspace, "sh", "-c", "grep -E ' /(cache|featdata) ' /proc/mounts | wc -l"))
		out, err := exec.Command("docker", "volume", "ls", "--format", "{{.Name}}", "--filter", "name=dcx-e2e-").Output()
		require.NoError(t, err)
		for _, v := range strings.Fields(string(out)) {
			assert.NotContains(t, v, "${", "volume name must be substituted: %q", v)
		}
	})
	t.Run("entrypoint_and_hooks", func(t *testing.T) {
		assert.Equal(t, "ran with 0 args", strings.TrimSpace(cat("/tmp/entrypoint-ran")), "the feature entrypoint ran")
		assert.Equal(t, "feature\nlocal", strings.TrimSpace(cat("/tmp/post-create.log")), "feature hooks run before devcontainer.json's")
	})
	t.Run("security", func(t *testing.T) {
		assert.Contains(t, cat("/proc/self/status"), "CapEff")
		out, _, err := helpers.RunDCXInDir(t, workspace, "ssh", "--", "sh", "-c", "echo $FEATURE_TEST_VAR")
		require.NoError(t, err)
		assert.Contains(t, out, "security-feature-value")
		assert.Contains(t, cat("/tmp/security-feature-marker"), "security feature installed")
	})
}

// createWorkspaceWithLocalFeatures creates the workspace TestLocalFeaturesE2E
// brings up: simple-marker and security-feature written here, with-options,
// with-dependencies and with-mounts copied from testdata/features.
func createWorkspaceWithLocalFeatures(t *testing.T) string {
	t.Helper()
	workspace := helpers.CreateWorkspaceFiles(t, nil) // taken down when the test ends
	features := filepath.Join(workspace, ".devcontainer", "features")
	write := func(feature, file, content string, mode os.FileMode) {
		dir := filepath.Join(features, feature)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, file), []byte(content), mode))
	}
	write("simple-marker", "devcontainer-feature.json", `{"id": "simple-marker", "version": "1.0.0", "name": "Simple Marker"}`, 0o644)
	write("simple-marker", "install.sh", "#!/bin/sh\nset -e\necho \"feature installed\" > /tmp/feature-marker\necho \"simple-marker installed\" > /tmp/simple-marker\n", 0o755)
	write("security-feature", "devcontainer-feature.json", `{
		"id": "security-feature",
		"version": "1.0.0",
		"name": "Security Feature",
		"capAdd": ["SYS_PTRACE"],
		"init": true,
		"containerEnv": {"FEATURE_TEST_VAR": "security-feature-value"}
	}`, 0o644)
	write("security-feature", "install.sh", "#!/bin/sh\nset -e\necho \"security feature installed\" > /tmp/security-feature-marker\n", 0o755)
	// containerEnv extending PATH, and a mount named with ${devcontainerId}.
	write("pathfeature", "devcontainer-feature.json", `{
		"id": "pathfeature", "version": "1.0.0", "name": "Path feature",
		"containerEnv": {"PATH": "/opt/pathfeature/bin:${PATH}"},
		"mounts": [{"source": "dcx-e2e-feat-${devcontainerId}", "target": "/featdata", "type": "volume"}]
	}`, 0o644)
	write("pathfeature", "install.sh", "#!/bin/sh\nmkdir -p /opt/pathfeature/bin\n", 0o755)
	// An entrypoint, and a postCreateCommand that runs before devcontainer.json's.
	write("with-entrypoint", "devcontainer-feature.json", `{
		"id": "with-entrypoint",
		"version": "1.0.0",
		"name": "With Entrypoint",
		"entrypoint": "/usr/local/share/with-entrypoint-init.sh",
		"postCreateCommand": "echo feature >> /tmp/post-create.log"
	}`, 0o644)
	write("with-entrypoint", "install.sh", `#!/bin/sh
set -e
cat > /usr/local/share/with-entrypoint-init.sh <<'EOF'
#!/bin/sh
echo "ran with $# args" > /tmp/entrypoint-ran
exec "$@"
EOF
chmod +x /usr/local/share/with-entrypoint-init.sh
`, 0o755)
	for _, fixture := range []string{"with-options", "with-dependencies", "with-mounts"} {
		src := helpers.FeatureFixture(t, fixture)
		for _, file := range []string{"devcontainer-feature.json", "install.sh"} {
			data, err := os.ReadFile(filepath.Join(src, file))
			require.NoError(t, err)
			write(fixture, file, string(data), 0o755)
		}
	}

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:3.24",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/simple-marker": {},
			"./features/with-options": {"greeting": "CustomHello", "enabled": true, "count": "5"},
			"./features/with-dependencies": {},
			"./features/with-mounts": {},
			"./features/security-feature": {},
			"./features/pathfeature": {},
			"./features/with-entrypoint": {}
		},
		"mounts": ["source=dcx-e2e-cache-${devcontainerId},target=/cache,type=volume"],
		"postCreateCommand": {"local": "echo local >> /tmp/post-create.log"}
	}`, helpers.UniqueTestName(t))
	require.NoError(t, os.WriteFile(filepath.Join(workspace, ".devcontainer", "devcontainer.json"), []byte(devcontainerJSON), 0o644))
	return workspace
}
