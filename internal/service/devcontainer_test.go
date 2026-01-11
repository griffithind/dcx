package service

import (
	"context"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockContainerClient implements state.ContainerClient for testing.
type mockContainerClient struct {
	containers      []state.ContainerSummary
	containerDetail *state.ContainerDetails
	listErr         error
	inspectErr      error
	stopErr         error
	removeErr       error
	stopCalled      bool
	removeCalled    bool
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

func (m *mockContainerClient) InspectContainer(ctx context.Context, containerID string) (*state.ContainerDetails, error) {
	if m.inspectErr != nil {
		return nil, m.inspectErr
	}
	return m.containerDetail, nil
}

func (m *mockContainerClient) StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error {
	m.stopCalled = true
	return m.stopErr
}

func (m *mockContainerClient) RemoveContainer(ctx context.Context, containerID string, force, removeVolumes bool) error {
	m.removeCalled = true
	return m.removeErr
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

			ids, err := svc.GetIdentifiers()
			require.NoError(t, err)
			assert.NotEmpty(t, ids.WorkspaceID)
			assert.Contains(t, ids.SSHHost, tt.wantSSHSuffix)
		})
	}
}

func TestUpOptions(t *testing.T) {
	tests := []struct {
		name        string
		opts        UpOptions
		wantRebuild bool
	}{
		{
			name:        "default options",
			opts:        UpOptions{},
			wantRebuild: false,
		},
		{
			name: "with rebuild",
			opts: UpOptions{
				Rebuild: true,
			},
			wantRebuild: true,
		},
		{
			name: "with all flags",
			opts: UpOptions{
				Rebuild:  true,
				Recreate: true,
				Pull:     true,
			},
			wantRebuild: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantRebuild, tt.opts.Rebuild)
		})
	}
}

func TestDownOptions(t *testing.T) {
	tests := []struct {
		name          string
		opts          DownOptions
		wantVolumes   bool
		wantOrphans   bool
	}{
		{
			name:        "default options",
			opts:        DownOptions{},
			wantVolumes: false,
			wantOrphans: false,
		},
		{
			name: "remove volumes",
			opts: DownOptions{
				RemoveVolumes: true,
			},
			wantVolumes: true,
			wantOrphans: false,
		},
		{
			name: "remove orphans",
			opts: DownOptions{
				RemoveOrphans: true,
			},
			wantVolumes: false,
			wantOrphans: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantVolumes, tt.opts.RemoveVolumes)
			assert.Equal(t, tt.wantOrphans, tt.opts.RemoveOrphans)
		})
	}
}

func TestPlanOptions(t *testing.T) {
	tests := []struct {
		name        string
		opts        PlanOptions
		wantRebuild bool
	}{
		{
			name:        "default",
			opts:        PlanOptions{},
			wantRebuild: false,
		},
		{
			name: "with rebuild",
			opts: PlanOptions{
				Rebuild: true,
			},
			wantRebuild: true,
		},
		{
			name: "with recreate",
			opts: PlanOptions{
				Recreate: true,
			},
			wantRebuild: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantRebuild, tt.opts.Rebuild)
		})
	}
}

func TestBuildOptions(t *testing.T) {
	tests := []struct {
		name      string
		opts      BuildOptions
		wantPull  bool
		wantCache bool
	}{
		{
			name:      "default",
			opts:      BuildOptions{},
			wantPull:  false,
			wantCache: false,
		},
		{
			name: "with pull",
			opts: BuildOptions{
				Pull: true,
			},
			wantPull:  true,
			wantCache: false,
		},
		{
			name: "no cache",
			opts: BuildOptions{
				NoCache: true,
			},
			wantPull:  false,
			wantCache: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantPull, tt.opts.Pull)
			assert.Equal(t, tt.wantCache, tt.opts.NoCache)
		})
	}
}

func TestPlanResult(t *testing.T) {
	tests := []struct {
		name       string
		state      state.ContainerState
		action     state.PlanAction
		wantReason bool
	}{
		{
			name:       "absent state",
			state:      state.StateAbsent,
			action:     state.PlanActionCreate,
			wantReason: true,
		},
		{
			name:       "running state",
			state:      state.StateRunning,
			action:     state.PlanActionNone,
			wantReason: true,
		},
		{
			name:       "stale state",
			state:      state.StateStale,
			action:     state.PlanActionRecreate,
			wantReason: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &PlanResult{
				State:  tt.state,
				Action: tt.action,
				Reason: "test reason",
			}
			assert.Equal(t, tt.state, result.State)
			assert.Equal(t, tt.action, result.Action)
			if tt.wantReason {
				assert.NotEmpty(t, result.Reason)
			}
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

func TestGetterMethods(t *testing.T) {
	t.Run("GetStateManager", func(t *testing.T) {
		mock := &mockContainerClient{}
		sm := state.NewStateManager(mock)
		svc := &DevContainerService{
			stateManager: sm,
		}
		assert.Equal(t, sm, svc.GetStateManager())
	})
}
