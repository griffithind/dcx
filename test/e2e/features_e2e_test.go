//go:build e2e

package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLocalFeatureE2E tests installing a local feature.
func TestLocalFeatureE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	// Create workspace with local feature
	workspace := createWorkspaceWithLocalFeature(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Build and run
	t.Run("up_installs_feature", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")

		state := helpers.GetContainerState(t, workspace)
		assert.Equal(t, "RUNNING", state)
	})

	// Verify feature was installed by checking for marker file
	t.Run("feature_installed", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/feature-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "feature installed")
	})
}

// TestLocalFeatureWithOptionsE2E tests a feature that accepts options.
func TestLocalFeatureWithOptionsE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	workspace := createWorkspaceWithOptionsFeature(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Build and run
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify feature options were passed
	t.Run("feature_options_passed", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/feature-options-marker")
		require.NoError(t, err)
		// Our config sets greeting to "CustomHello"
		assert.Contains(t, stdout, "GREETING=CustomHello")
		assert.Contains(t, stdout, "ENABLED=true")
		assert.Contains(t, stdout, "COUNT=5")
	})
}

// TestLocalFeatureWithDependenciesE2E tests feature installation ordering.
func TestLocalFeatureWithDependenciesE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	workspace := createWorkspaceWithDependentFeatures(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Build and run
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify both features were installed
	t.Run("simple_marker_installed", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/simple-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "simple-marker installed")
	})

	// Verify the dependent feature found its dependency
	t.Run("dependent_feature_installed", func(t *testing.T) {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/feature-deps-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "MESSAGE=depends")
	})
}

// TestMultipleFeaturesE2E tests installing multiple features.
func TestMultipleFeaturesE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	workspace := createWorkspaceWithMultipleFeatures(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Build and run
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify all features installed
	t.Run("all_features_installed", func(t *testing.T) {
		// Check simple-marker
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/simple-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "simple-marker installed")

		// Check with-options marker
		stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/feature-options-marker")
		require.NoError(t, err)
		assert.Contains(t, stdout, "GREETING")
	})
}

// createWorkspaceWithLocalFeature creates a workspace with a simple local feature.
func createWorkspaceWithLocalFeature(t *testing.T) string {
	t.Helper()

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create feature directory
	featureDir := filepath.Join(devcontainerDir, "features", "simple-marker")
	err = os.MkdirAll(featureDir, 0755)
	require.NoError(t, err)

	// Create feature metadata
	featureJSON := `{
		"id": "simple-marker",
		"version": "1.0.0",
		"name": "Simple Marker Feature",
		"description": "Creates a marker file"
	}`
	err = os.WriteFile(filepath.Join(featureDir, "devcontainer-feature.json"), []byte(featureJSON), 0644)
	require.NoError(t, err)

	// Create install script (use /bin/sh for Alpine compatibility)
	installScript := `#!/bin/sh
set -e
echo "feature installed" > /tmp/feature-marker
`
	err = os.WriteFile(filepath.Join(featureDir, "install.sh"), []byte(installScript), 0755)
	require.NoError(t, err)

	// Create devcontainer.json
	devcontainerJSON := `{
		"name": "Feature Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/simple-marker": {}
		}
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	return workspace
}

// createWorkspaceWithOptionsFeature creates a workspace with the with-options feature.
func createWorkspaceWithOptionsFeature(t *testing.T) string {
	t.Helper()

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create feature directory
	featureDir := filepath.Join(devcontainerDir, "features", "with-options")
	err = os.MkdirAll(featureDir, 0755)
	require.NoError(t, err)

	// Copy feature from testdata
	featureFixture := helpers.FeatureFixture(t, "with-options")

	// Copy devcontainer-feature.json
	data, err := os.ReadFile(filepath.Join(featureFixture, "devcontainer-feature.json"))
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(featureDir, "devcontainer-feature.json"), data, 0644)
	require.NoError(t, err)

	// Copy install.sh
	data, err = os.ReadFile(filepath.Join(featureFixture, "install.sh"))
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(featureDir, "install.sh"), data, 0755)
	require.NoError(t, err)

	// Create devcontainer.json with options
	devcontainerJSON := `{
		"name": "Options Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/with-options": {
				"greeting": "CustomHello",
				"enabled": true,
				"count": "5"
			}
		}
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	return workspace
}

// createWorkspaceWithDependentFeatures creates a workspace with features that have dependencies.
func createWorkspaceWithDependentFeatures(t *testing.T) string {
	t.Helper()

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	featuresDir := filepath.Join(devcontainerDir, "features")
	err := os.MkdirAll(featuresDir, 0755)
	require.NoError(t, err)

	// Create simple-marker feature
	simpleDir := filepath.Join(featuresDir, "simple-marker")
	err = os.MkdirAll(simpleDir, 0755)
	require.NoError(t, err)

	simpleJSON := `{
		"id": "simple-marker",
		"version": "1.0.0",
		"name": "Simple Marker"
	}`
	err = os.WriteFile(filepath.Join(simpleDir, "devcontainer-feature.json"), []byte(simpleJSON), 0644)
	require.NoError(t, err)

	simpleInstall := `#!/bin/sh
set -e
echo "simple-marker installed" > /tmp/simple-marker
`
	err = os.WriteFile(filepath.Join(simpleDir, "install.sh"), []byte(simpleInstall), 0755)
	require.NoError(t, err)

	// Create with-dependencies feature
	depsDir := filepath.Join(featuresDir, "with-dependencies")
	err = os.MkdirAll(depsDir, 0755)
	require.NoError(t, err)

	depsFeatureFixture := helpers.FeatureFixture(t, "with-dependencies")

	// Copy files from fixture
	data, err := os.ReadFile(filepath.Join(depsFeatureFixture, "devcontainer-feature.json"))
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(depsDir, "devcontainer-feature.json"), data, 0644)
	require.NoError(t, err)

	data, err = os.ReadFile(filepath.Join(depsFeatureFixture, "install.sh"))
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(depsDir, "install.sh"), data, 0755)
	require.NoError(t, err)

	// Create devcontainer.json
	devcontainerJSON := `{
		"name": "Dependencies Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/with-dependencies": {},
			"./features/simple-marker": {}
		}
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	return workspace
}

// createWorkspaceWithMultipleFeatures creates a workspace with multiple independent features.
func createWorkspaceWithMultipleFeatures(t *testing.T) string {
	t.Helper()

	workspace := t.TempDir()

	// Create .devcontainer directory
	devcontainerDir := filepath.Join(workspace, ".devcontainer")
	featuresDir := filepath.Join(devcontainerDir, "features")
	err := os.MkdirAll(featuresDir, 0755)
	require.NoError(t, err)

	// Create simple-marker feature
	simpleDir := filepath.Join(featuresDir, "simple-marker")
	err = os.MkdirAll(simpleDir, 0755)
	require.NoError(t, err)

	simpleJSON := `{
		"id": "simple-marker",
		"version": "1.0.0",
		"name": "Simple Marker"
	}`
	err = os.WriteFile(filepath.Join(simpleDir, "devcontainer-feature.json"), []byte(simpleJSON), 0644)
	require.NoError(t, err)

	simpleInstall := `#!/bin/sh
set -e
echo "simple-marker installed" > /tmp/simple-marker
`
	err = os.WriteFile(filepath.Join(simpleDir, "install.sh"), []byte(simpleInstall), 0755)
	require.NoError(t, err)

	// Create with-options feature
	optionsDir := filepath.Join(featuresDir, "with-options")
	err = os.MkdirAll(optionsDir, 0755)
	require.NoError(t, err)

	optionsFixture := helpers.FeatureFixture(t, "with-options")

	data, err := os.ReadFile(filepath.Join(optionsFixture, "devcontainer-feature.json"))
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(optionsDir, "devcontainer-feature.json"), data, 0644)
	require.NoError(t, err)

	data, err = os.ReadFile(filepath.Join(optionsFixture, "install.sh"))
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(optionsDir, "install.sh"), data, 0755)
	require.NoError(t, err)

	// Create devcontainer.json
	devcontainerJSON := `{
		"name": "Multiple Features Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/simple-marker": {},
			"./features/with-options": {}
		}
	}`
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	return workspace
}

// TestFeatureCachingE2E tests that features are cached between runs.
func TestFeatureCachingE2E(t *testing.T) {
	helpers.RequireDockerAvailable(t)

	workspace := createWorkspaceWithOptionsFeature(t)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// First up - features should be installed
	t.Run("first_up_installs_features", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")
		// Should see feature building output
		assert.Contains(t, stdout, "Building derived image")
	})

	// Second up - features should be cached (no "Building derived image" message expected
	// since the container is already running and config hasn't changed)
	t.Run("second_up_uses_cache", func(t *testing.T) {
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		// Should see that environment is already running
		assert.Contains(t, stdout, "already running")
	})

	// Now change the feature options and verify rebuild happens
	t.Run("config_change_triggers_rebuild", func(t *testing.T) {
		// First tear down
		helpers.RunDCXInDirSuccess(t, workspace, "down")

		// Change the feature options in devcontainer.json
		devcontainerDir := filepath.Join(workspace, ".devcontainer")
		devcontainerJSON := `{
		"name": "Options Test",
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"features": {
			"./features/with-options": {
				"greeting": "ChangedHello",
				"enabled": true,
				"count": "10"
			}
		}
	}`
		err := os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
		require.NoError(t, err)

		// Now up should rebuild since config changed
		stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
		assert.Contains(t, stdout, "Environment is ready")
		// Should see building output since config changed
		assert.Contains(t, stdout, "Building derived image")

		// Verify new options are in effect
		execStdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/tmp/feature-options-marker")
		require.NoError(t, err)
		assert.Contains(t, execStdout, "GREETING=ChangedHello")
		assert.Contains(t, execStdout, "COUNT=10")
	})
}
