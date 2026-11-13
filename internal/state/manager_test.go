package state

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockContainerClient is a test double for ContainerClient.
type mockContainerClient struct {
	containers []ContainerSummary
	listErr    error
}

func (m *mockContainerClient) ListContainersWithLabels(_ context.Context, labels map[string]string) ([]ContainerSummary, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}

	var result []ContainerSummary
	for _, c := range m.containers {
		match := true
		for k, v := range labels {
			if c.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			result = append(result, c)
		}
	}
	return result, nil
}

func TestGetStateWithHash(t *testing.T) {
	t.Run("returns stale when config hash differs", func(t *testing.T) {
		client := &mockContainerClient{
			containers: []ContainerSummary{
				{
					ID: "abc123", Name: "test", State: "running", Running: true,
					Labels: map[string]string{
						LabelWorkspaceID: "test-workspace",
						LabelIsPrimary:   "true",
						LabelHashConfig:  "old-hash",
						LabelHashVersion: CurrentHashVersion,
					},
				},
			},
		}

		mgr := NewStateManager(client)
		state, _, err := mgr.GetStateWithHash(
			context.Background(), "test-workspace", "new-hash")

		require.NoError(t, err)
		assert.Equal(t, StateStale, state)
	})

	t.Run("returns running when config hash matches", func(t *testing.T) {
		client := &mockContainerClient{
			containers: []ContainerSummary{
				{
					ID: "abc123", Name: "test", State: "running", Running: true,
					Labels: map[string]string{
						LabelWorkspaceID: "test-workspace",
						LabelIsPrimary:   "true",
						LabelHashConfig:  "same-hash",
						LabelHashVersion: CurrentHashVersion,
					},
				},
			},
		}

		mgr := NewStateManager(client)
		state, _, err := mgr.GetStateWithHash(
			context.Background(), "test-workspace", "same-hash")

		require.NoError(t, err)
		assert.Equal(t, StateRunning, state)
	})

	t.Run("returns absent when no containers found", func(t *testing.T) {
		client := &mockContainerClient{}

		mgr := NewStateManager(client)
		state, info, err := mgr.GetStateWithHash(
			context.Background(), "test-workspace", "any-hash")

		require.NoError(t, err)
		assert.Equal(t, StateAbsent, state)
		assert.Nil(t, info)
	})

	t.Run("not stale when no hash stored on container", func(t *testing.T) {
		client := &mockContainerClient{
			containers: []ContainerSummary{
				{
					ID: "abc123", Name: "test", State: "running", Running: true,
					Labels: map[string]string{
						LabelWorkspaceID: "test-workspace",
						LabelIsPrimary:   "true",
					},
				},
			},
		}

		mgr := NewStateManager(client)
		state, _, err := mgr.GetStateWithHash(
			context.Background(), "test-workspace", "any-hash")

		require.NoError(t, err)
		assert.Equal(t, StateRunning, state, "should not report stale when no stored hash exists")
	})

	t.Run("detects staleness from any build input change", func(t *testing.T) {
		// The config hash covers all inputs (devcontainer.json, Dockerfiles,
		// compose files, features). A change to ANY input produces a different
		// hash, which triggers staleness.
		client := &mockContainerClient{
			containers: []ContainerSummary{
				{
					ID: "abc123", Name: "test", State: "running", Running: true,
					Labels: map[string]string{
						LabelWorkspaceID: "test-workspace",
						LabelIsPrimary:   "true",
						LabelHashConfig:  "hash-before-dockerfile-change",
						LabelHashVersion: CurrentHashVersion,
					},
				},
			},
		}

		mgr := NewStateManager(client)
		state, _, err := mgr.GetStateWithHash(
			context.Background(), "test-workspace", "hash-after-dockerfile-change")

		require.NoError(t, err)
		assert.Equal(t, StateStale, state)
	})
}

func TestContainerInfoConfigHash(t *testing.T) {
	t.Run("populates from label", func(t *testing.T) {
		summary := &ContainerSummary{
			ID: "abc123", Name: "test", State: "running",
			Labels: map[string]string{
				LabelHashConfig:  "the-hash",
				LabelHashVersion: CurrentHashVersion,
				LabelIsPrimary:   "true",
			},
		}

		info := containerInfoFromSummary(summary)
		assert.Equal(t, "the-hash", info.ConfigHash)
	})

	t.Run("empty when no hash label set", func(t *testing.T) {
		summary := &ContainerSummary{
			ID: "abc123", Name: "test", State: "running",
			Labels: map[string]string{
				LabelIsPrimary: "true",
			},
		}

		info := containerInfoFromSummary(summary)
		assert.Empty(t, info.ConfigHash)
	})
}

// Containers created before the hash format changed carry no version label;
// their hash can't be compared, so they must not be recreated on upgrade.
func TestLegacyHashIsNotStale(t *testing.T) {
	client := &mockContainerClient{containers: []ContainerSummary{{
		ID: "abc123", Name: "test", State: "running", Running: true,
		Labels: map[string]string{
			LabelWorkspaceID: "ws",
			LabelIsPrimary:   "true",
			LabelHashConfig:  "hash-from-old-dcx",
		},
	}}}

	state, info, err := NewStateManager(client).GetStateWithHash(context.Background(), "ws", "new-hash")
	require.NoError(t, err)
	assert.Equal(t, StateRunning, state)
	assert.True(t, info.IsLegacyHash())
}
