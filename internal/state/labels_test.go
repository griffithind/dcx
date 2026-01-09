package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewContainerLabels(t *testing.T) {
	labels := NewContainerLabels()

	assert.Equal(t, SchemaVersion, labels.SchemaVersion)
	assert.True(t, labels.Managed)
	assert.NotNil(t, labels.FeaturesInstalled)
	assert.NotNil(t, labels.FeaturesConfig)
	assert.NotNil(t, labels.CacheFeatureDigests)
}

func TestContainerLabelsRoundtrip(t *testing.T) {
	t.Run("basic fields", func(t *testing.T) {
		original := NewContainerLabels()
		original.WorkspaceID = "abc123"
		original.WorkspaceName = "my-project"
		original.WorkspacePath = "/home/user/projects/my-project"
		original.ConfigPath = ".devcontainer/devcontainer.json"
		original.HashConfig = "hash123"
		original.BuildMethod = BuildMethodImage

		m := original.ToMap()
		restored := ContainerLabelsFromMap(m)

		assert.Equal(t, original.WorkspaceID, restored.WorkspaceID)
		assert.Equal(t, original.WorkspaceName, restored.WorkspaceName)
		assert.Equal(t, original.WorkspacePath, restored.WorkspacePath)
		assert.Equal(t, original.ConfigPath, restored.ConfigPath)
		assert.Equal(t, original.HashConfig, restored.HashConfig)
		assert.Equal(t, original.BuildMethod, restored.BuildMethod)
	})

	t.Run("timestamps", func(t *testing.T) {
		original := NewContainerLabels()
		original.CreatedAt = time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
		original.LastStartedAt = time.Date(2024, 1, 16, 14, 0, 0, 0, time.UTC)

		m := original.ToMap()
		restored := ContainerLabelsFromMap(m)

		assert.Equal(t, original.CreatedAt, restored.CreatedAt)
		assert.Equal(t, original.LastStartedAt, restored.LastStartedAt)
	})

	t.Run("features", func(t *testing.T) {
		original := NewContainerLabels()
		original.FeaturesInstalled = []string{"ghcr.io/devcontainers/features/go:1", "ghcr.io/devcontainers/features/node:1"}
		original.FeaturesConfig = map[string]map[string]interface{}{
			"go":   {"version": "1.21"},
			"node": {"version": "20"},
		}

		m := original.ToMap()
		restored := ContainerLabelsFromMap(m)

		assert.Equal(t, original.FeaturesInstalled, restored.FeaturesInstalled)
		assert.Equal(t, len(original.FeaturesConfig), len(restored.FeaturesConfig))
	})

	t.Run("compose fields", func(t *testing.T) {
		original := NewContainerLabels()
		original.ComposeProject = "my-compose-project"
		original.ComposeService = "app"
		original.IsPrimary = true

		m := original.ToMap()
		restored := ContainerLabelsFromMap(m)

		assert.Equal(t, original.ComposeProject, restored.ComposeProject)
		assert.Equal(t, original.ComposeService, restored.ComposeService)
		assert.Equal(t, original.IsPrimary, restored.IsPrimary)
	})
}

func TestFilterSelectors(t *testing.T) {
	t.Run("FilterSelector", func(t *testing.T) {
		filter := FilterSelector()
		assert.Contains(t, filter, LabelManaged)
		assert.Contains(t, filter, "true")
	})

	t.Run("FilterByWorkspace", func(t *testing.T) {
		filter := FilterByWorkspace("abc123")
		assert.Contains(t, filter, LabelWorkspaceID)
		assert.Contains(t, filter, "abc123")
	})

	t.Run("FilterByComposeProject", func(t *testing.T) {
		filter := FilterByComposeProject("my-project")
		assert.Contains(t, filter, LabelComposeProject)
		assert.Contains(t, filter, "my-project")
	})
}

func TestIsDCXManaged(t *testing.T) {
	t.Run("new format", func(t *testing.T) {
		m := map[string]string{LabelManaged: "true"}
		assert.True(t, IsDCXManaged(m))
	})

	t.Run("legacy format", func(t *testing.T) {
		m := map[string]string{LegacyPrefix + "managed": "true"}
		assert.True(t, IsDCXManaged(m))
	})

	t.Run("unmanaged", func(t *testing.T) {
		m := map[string]string{"some.other.label": "value"}
		assert.False(t, IsDCXManaged(m))
	})
}

func TestGetWorkspaceID(t *testing.T) {
	t.Run("new format", func(t *testing.T) {
		m := map[string]string{LabelWorkspaceID: "new-id"}
		assert.Equal(t, "new-id", GetWorkspaceID(m))
	})

	t.Run("legacy format", func(t *testing.T) {
		m := map[string]string{LegacyPrefix + "env_key": "legacy-id"}
		assert.Equal(t, "legacy-id", GetWorkspaceID(m))
	})

	t.Run("prefers new format", func(t *testing.T) {
		m := map[string]string{
			LabelWorkspaceID:           "new-id",
			LegacyPrefix + "env_key": "legacy-id",
		}
		assert.Equal(t, "new-id", GetWorkspaceID(m))
	})
}

func TestMigrateFromLegacy(t *testing.T) {
	legacy := map[string]string{
		LegacyPrefix + "managed":        "true",
		LegacyPrefix + "env_key":        "workspace-123",
		LegacyPrefix + "workspace_path": "/home/user/project",
		LegacyPrefix + "config_hash":    "abc123",
		LegacyPrefix + "plan":           "compose",
		LegacyPrefix + "primary":        "true",
		LegacyPrefix + "compose_project": "my-project",
		LegacyPrefix + "primary_service": "app",
	}

	migrated := MigrateFromLegacy(legacy)

	require.NotNil(t, migrated)
	assert.True(t, migrated.Managed)
	assert.Equal(t, "workspace-123", migrated.WorkspaceID)
	assert.Equal(t, "/home/user/project", migrated.WorkspacePath)
	assert.Equal(t, "abc123", migrated.HashConfig)
	assert.Equal(t, BuildMethodCompose, migrated.BuildMethod)
	assert.True(t, migrated.IsPrimary)
	assert.Equal(t, "my-project", migrated.ComposeProject)
	assert.Equal(t, "app", migrated.ComposeService)
}
