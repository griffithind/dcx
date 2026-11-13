package service

import (
	"context"
	"testing"

	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockContainerClient implements state.ContainerClient for testing.
type mockContainerClient struct {
	containers []state.ContainerSummary
	listErr    error
}

func (m *mockContainerClient) ListContainersWithLabels(ctx context.Context, labels map[string]string) ([]state.ContainerSummary, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	// Filter by labels if provided
	if len(labels) == 0 {
		return m.containers, nil
	}
	var filtered []state.ContainerSummary
	for _, c := range m.containers {
		matches := true
		for k, v := range labels {
			if c.Labels[k] != v {
				matches = false
				break
			}
		}
		if matches {
			filtered = append(filtered, c)
		}
	}
	return filtered, nil
}

func TestIdentifiers(t *testing.T) {
	tests := []struct {
		name          string
		workspacePath string
		wantSSHSuffix string
	}{
		{
			name:          "basic workspace path",
			workspacePath: "/tmp/test-workspace",
			wantSSHSuffix: ".dcx",
		},
		{
			name:          "workspace with special chars",
			workspacePath: "/home/user/my-project",
			wantSSHSuffix: ".dcx",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create service with nil docker client (we only need workspace path for identifiers)
			svc := &DevContainerService{
				workspacePath: tt.workspacePath,
			}

			ids := svc.Identifiers()
			assert.NotEmpty(t, ids.WorkspaceID)
			assert.Contains(t, ids.SSHHost, tt.wantSSHSuffix)
		})
	}
}

func TestStateManagerIntegration(t *testing.T) {
	tests := []struct {
		name       string
		containers []state.ContainerSummary
		wantState  state.ContainerState
	}{
		{
			name:       "no containers",
			containers: nil,
			wantState:  state.StateAbsent,
		},
		{
			name: "running container",
			containers: []state.ContainerSummary{
				{
					ID:      "abc123",
					Name:    "test-container",
					State:   "running",
					Running: true,
					Labels: map[string]string{
						state.LabelWorkspaceID: "test-ws",
						state.LabelIsPrimary:   "true",
						state.LabelManaged:     "true",
					},
				},
			},
			wantState: state.StateRunning,
		},
		{
			name: "stopped container",
			containers: []state.ContainerSummary{
				{
					ID:      "abc123",
					Name:    "test-container",
					State:   "exited",
					Running: false,
					Labels: map[string]string{
						state.LabelWorkspaceID: "test-ws",
						state.LabelIsPrimary:   "true",
						state.LabelManaged:     "true",
					},
				},
			},
			wantState: state.StateCreated,
		},
		{
			name: "container without primary label",
			containers: []state.ContainerSummary{
				{
					ID:      "abc123",
					Name:    "test-container",
					State:   "running",
					Running: true,
					Labels: map[string]string{
						state.LabelWorkspaceID: "test-ws",
						state.LabelManaged:     "true",
					},
				},
			},
			wantState: state.StateBroken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockContainerClient{containers: tt.containers}
			sm := state.NewStateManager(mock)

			gotState, _, err := sm.GetState(context.Background(), "test-ws")
			require.NoError(t, err)
			assert.Equal(t, tt.wantState, gotState)
		})
	}
}
