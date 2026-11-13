package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewContainerLabels(t *testing.T) {
	labels := NewContainerLabels()

	assert.Equal(t, SchemaVersion, labels.SchemaVersion)
	assert.True(t, labels.Managed)
	assert.NotNil(t, labels.FeaturesInstalled)
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

	t.Run("features", func(t *testing.T) {
		original := NewContainerLabels()
		original.FeaturesInstalled = []string{"ghcr.io/devcontainers/features/go:1", "ghcr.io/devcontainers/features/node:1"}

		m := original.ToMap()
		restored := ContainerLabelsFromMap(m)

		assert.Equal(t, original.FeaturesInstalled, restored.FeaturesInstalled)
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
