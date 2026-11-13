package state

import (
	"context"
	"time"
)

// ContainerClient is the interface for container operations needed by the state manager.
// This avoids a dependency on the container package and allows for easier testing.
type ContainerClient interface {
	// ListContainersWithLabels returns containers matching label filters.
	// Returns a list of container summaries with ID, Name, State, and Labels.
	ListContainersWithLabels(ctx context.Context, labels map[string]string) ([]ContainerSummary, error)

	// InspectContainer returns detailed information about a container.
	InspectContainer(ctx context.Context, containerID string) (*ContainerDetails, error)

	// StopContainer stops a running container.
	StopContainer(ctx context.Context, containerID string, timeout *time.Duration) error

	// RemoveContainer removes a container.
	RemoveContainer(ctx context.Context, containerID string, force, removeVolumes bool) error
}

// ContainerSummary is a minimal container summary returned by ListContainersWithLabels.
type ContainerSummary struct {
	ID      string
	Name    string
	State   string
	Running bool
	Labels  map[string]string
	Created time.Time
}

// ContainerDetails is detailed container info returned by InspectContainer.
type ContainerDetails struct {
	ID         string
	Name       string
	State      string
	Running    bool
	StartedAt  string
	Image      string
	Labels     map[string]string
	Mounts     []string
	WorkingDir string
	// Env is the container's environment (image ENV plus containerEnv) as
	// KEY=VALUE pairs.
	Env []string
}

// StateManager handles state detection and management for devcontainer environments.
// This replaces the previous containerstate.Manager with clearer naming.
type StateManager struct {
	client ContainerClient
}

// NewStateManager creates a new state manager.
func NewStateManager(client ContainerClient) *StateManager {
	return &StateManager{client: client}
}

// GetState determines the current state of the devcontainer environment.
func (m *StateManager) GetState(ctx context.Context, workspaceID string) (ContainerState, *ContainerInfo, error) {
	containers, err := m.client.ListContainersWithLabels(ctx, map[string]string{
		LabelWorkspaceID: workspaceID,
	})
	if err != nil {
		return StateAbsent, nil, err
	}

	// No containers found
	if len(containers) == 0 {
		return StateAbsent, nil, nil
	}

	// Find the primary container
	var primary *ContainerSummary
	for i := range containers {
		if IsPrimaryContainer(containers[i].Labels) {
			primary = &containers[i]
			break
		}
	}

	// No primary container found - broken state
	if primary == nil {
		// Return info about first container for debugging
		if len(containers) > 0 {
			info := containerInfoFromSummary(&containers[0])
			return StateBroken, info, nil
		}
		return StateBroken, nil, nil
	}

	// Get container info
	info := containerInfoFromSummary(primary)

	// Check if running
	if primary.State == "running" || primary.Running {
		return StateRunning, info, nil
	}

	return StateCreated, info, nil
}

// containerInfoFromSummary creates ContainerInfo from a ContainerSummary.
func containerInfoFromSummary(c *ContainerSummary) *ContainerInfo {
	l := ContainerLabelsFromMap(c.Labels)

	return &ContainerInfo{
		ID:             c.ID,
		Name:           c.Name,
		Status:         c.State,
		Running:        c.Running || c.State == "running",
		ConfigHash:     l.HashConfig,
		HashVersion:    l.HashVersion,
		WorkspaceID:    l.WorkspaceID,
		Plan:           l.BuildMethod,
		ComposeProject: l.ComposeProject,
		PrimaryService: l.ComposeService,
		Labels:         l,
	}
}

// GetStateWithProject returns the state of the workspace's containers.
//
// Containers are always identified by the workspace ID label, which is
// independent of names, so environments created by older dcx versions
// (whose container/project names were derived from "name" alone) are still
// found. projectName is accepted for API compatibility and not used for the
// lookup.
func (m *StateManager) GetStateWithProject(ctx context.Context, projectName, workspaceID string) (ContainerState, *ContainerInfo, error) {
	_ = projectName
	return m.GetState(ctx, workspaceID)
}

// CurrentHashVersion is stamped on containers as LabelHashVersion.
const CurrentHashVersion = "2"

// IsLegacyHash reports whether the container's config hash was computed by
// an older dcx and therefore cannot be used to detect config changes.
func (c *ContainerInfo) IsLegacyHash() bool {
	return c != nil && c.HashVersion != CurrentHashVersion
}

// GetStateWithProjectAndHash combines project lookup with hash check.
func (m *StateManager) GetStateWithProjectAndHash(ctx context.Context, projectName, workspaceID, currentConfigHash string) (ContainerState, *ContainerInfo, error) {
	state, info, err := m.GetStateWithProject(ctx, projectName, workspaceID)
	if err != nil || info == nil {
		return state, info, err
	}

	// Hashes from another algorithm version can't be compared; treat such
	// containers as current rather than recreating every environment on
	// upgrade (callers can point users at --recreate, see IsLegacyHash).
	if info.ConfigHash != "" && info.HashVersion == CurrentHashVersion && info.ConfigHash != currentConfigHash {
		return StateStale, info, nil
	}

	return state, info, nil
}
