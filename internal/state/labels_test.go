package state

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
