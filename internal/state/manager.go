package state

import (
	"context"
	"time"

	"github.com/griffithind/dcx/internal/common"
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
		WorkspaceID:    l.WorkspaceID,
		Plan:           l.BuildMethod,
		ComposeProject: l.ComposeProject,
		PrimaryService: l.ComposeService,
		Labels:         l,
	}
}

// GetStateWithHashCheck determines state and checks for staleness.
func (m *StateManager) GetStateWithHashCheck(ctx context.Context, workspaceID, currentConfigHash string) (ContainerState, *ContainerInfo, error) {
	state, info, err := m.GetState(ctx, workspaceID)
	if err != nil || info == nil {
		return state, info, err
	}

	// Check if config has changed
	if info.ConfigHash != "" && info.ConfigHash != currentConfigHash {
		return StateStale, info, nil
	}

	return state, info, nil
}

// GetStateWithProject handles lookup for both project-named and workspace ID containers.
func (m *StateManager) GetStateWithProject(ctx context.Context, projectName, workspaceID string) (ContainerState, *ContainerInfo, error) {
	// First try project name if set
	if projectName != "" {
		sanitized := common.SanitizeProjectName(projectName)
		containers, err := m.client.ListContainersWithLabels(ctx, map[string]string{
			LabelWorkspaceID: sanitized,
		})
		if err == nil && len(containers) > 0 {
			return m.processContainers(containers)
		}
	}

	// Fall back to workspace ID lookup
	return m.GetState(ctx, workspaceID)
}

// processContainers extracts state and info from a list of containers.
func (m *StateManager) processContainers(containers []ContainerSummary) (ContainerState, *ContainerInfo, error) {
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
		if len(containers) > 0 {
			info := containerInfoFromSummary(&containers[0])
			return StateBroken, info, nil
		}
		return StateBroken, nil, nil
	}

	info := containerInfoFromSummary(primary)

	if primary.State == "running" || primary.Running {
		return StateRunning, info, nil
	}

	return StateCreated, info, nil
}

// GetStateWithProjectAndHash combines project lookup with hash check.
func (m *StateManager) GetStateWithProjectAndHash(ctx context.Context, projectName, workspaceID, currentConfigHash string) (ContainerState, *ContainerInfo, error) {
	state, info, err := m.GetStateWithProject(ctx, projectName, workspaceID)
	if err != nil || info == nil {
		return state, info, err
	}

	// Check if config has changed
	if info.ConfigHash != "" && info.ConfigHash != currentConfigHash {
		return StateStale, info, nil
	}

	return state, info, nil
}

// FindContainers returns all containers for an environment.
func (m *StateManager) FindContainers(ctx context.Context, workspaceID string) ([]ContainerInfo, error) {
	containers, err := m.client.ListContainersWithLabels(ctx, map[string]string{
		LabelWorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}

	result := make([]ContainerInfo, 0, len(containers))
	for i := range containers {
		result = append(result, *containerInfoFromSummary(&containers[i]))
	}

	return result, nil
}

// FindPrimaryContainer returns the primary container for an environment.
func (m *StateManager) FindPrimaryContainer(ctx context.Context, workspaceID string) (*ContainerInfo, error) {
	containers, err := m.client.ListContainersWithLabels(ctx, map[string]string{
		LabelWorkspaceID: workspaceID,
		LabelIsPrimary:   "true",
	})
	if err != nil {
		return nil, err
	}

	if len(containers) == 0 {
		return nil, nil
	}

	return containerInfoFromSummary(&containers[0]), nil
}

// FindContainerByName returns a container by its name.
// This is used by the SSH command to find a specific container.
func (m *StateManager) FindContainerByName(ctx context.Context, containerName string) (*ContainerInfo, error) {
	containers, err := m.client.ListContainersWithLabels(ctx, map[string]string{
		LabelManaged: "true",
	})
	if err != nil {
		return nil, err
	}

	for i := range containers {
		if containers[i].Name == containerName {
			return containerInfoFromSummary(&containers[i]), nil
		}
	}

	return nil, nil
}

// Cleanup removes all containers for an environment.
// This is useful for recovering from broken states.
// If removeVolumes is true, anonymous volumes attached to containers are also removed.
func (m *StateManager) Cleanup(ctx context.Context, workspaceID string, removeVolumes bool) error {
	containers, err := m.client.ListContainersWithLabels(ctx, map[string]string{
		LabelWorkspaceID: workspaceID,
	})
	if err != nil {
		return err
	}

	var lastErr error
	for _, c := range containers {
		// Stop if running
		if c.Running || c.State == "running" {
			if err := m.client.StopContainer(ctx, c.ID, nil); err != nil {
				lastErr = err
				continue
			}
		}

		// Remove container and optionally volumes
		if err := m.client.RemoveContainer(ctx, c.ID, true, removeVolumes); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// ValidateState checks if the current state allows the requested operation.
func (m *StateManager) ValidateState(ctx context.Context, workspaceID string, operation Operation) error {
	state, _, err := m.GetState(ctx, workspaceID)
	if err != nil {
		return err
	}

	switch operation {
	case OpStart:
		if state == StateRunning {
			return ErrAlreadyRunning
		}
		if state == StateAbsent {
			return ErrNoContainer
		}
		if state == StateStale {
			return ErrStaleConfig
		}
		if state == StateBroken {
			return ErrBrokenState
		}
	case OpStop:
		if state != StateRunning {
			return ErrNotRunning
		}
	case OpExec:
		if state != StateRunning {
			return ErrNotRunning
		}
	case OpDown:
		if state == StateAbsent {
			return ErrNoContainer
		}
	case OpUp:
		// Up can be run in any state
	}

	return nil
}

// GetDiagnostics returns diagnostic information for troubleshooting.
func (m *StateManager) GetDiagnostics(ctx context.Context, workspaceID string) (*Diagnostics, error) {
	state, info, err := m.GetState(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	containers, err := m.FindContainers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	diag := &Diagnostics{
		State:      state,
		Recovery:   state.GetRecovery(),
		Containers: containers,
	}

	if info != nil {
		diag.PrimaryContainer = info
	}

	return diag, nil
}
