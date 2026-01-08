package state

import (
	"context"

	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/workspace"
)

// Manager handles state detection and management for devcontainer environments.
type Manager struct {
	client *docker.Client
}

// NewManager creates a new state manager.
func NewManager(client *docker.Client) *Manager {
	return &Manager{client: client}
}

// ResolveIdentifier returns the project name if configured, otherwise computes workspace ID.
// The projectName parameter is from dcx.json configuration.
func ResolveIdentifier(workspacePath string, projectName string) string {
	if projectName != "" {
		return docker.SanitizeProjectName(projectName)
	}
	return workspace.ComputeID(workspacePath)
}

// GetState determines the current state of the devcontainer environment.
func (m *Manager) GetState(ctx context.Context, workspaceID string) (State, *ContainerInfo, error) {
	// Try new labels first (com.griffithind.dcx.workspace.id)
	containers, err := m.client.ListContainers(ctx, map[string]string{
		labels.LabelWorkspaceID: workspaceID,
	})
	if err != nil {
		return StateAbsent, nil, err
	}

	// Fall back to legacy labels (io.github.dcx.env_key)
	if len(containers) == 0 {
		containers, err = m.client.ListContainers(ctx, map[string]string{
			docker.LabelEnvKey: workspaceID,
		})
		if err != nil {
			return StateAbsent, nil, err
		}
	}

	// No containers found
	if len(containers) == 0 {
		return StateAbsent, nil, nil
	}

	// Find the primary container (check both new and legacy labels)
	var primary *docker.Container
	for i := range containers {
		if isPrimaryContainer(containers[i].Labels) {
			primary = &containers[i]
			break
		}
	}

	// No primary container found - broken state
	if primary == nil {
		// Return info about first container for debugging
		if len(containers) > 0 {
			info := containerInfoFromDocker(&containers[0])
			return StateBroken, info, nil
		}
		return StateBroken, nil, nil
	}

	// Get container info
	info := containerInfoFromDocker(primary)

	// Check if running
	if primary.Running {
		return StateRunning, info, nil
	}

	return StateCreated, info, nil
}

// GetStateWithHashCheck determines state and checks for staleness.
func (m *Manager) GetStateWithHashCheck(ctx context.Context, envKey, currentConfigHash string) (State, *ContainerInfo, error) {
	state, info, err := m.GetState(ctx, envKey)
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
// This enables migration from hash-based naming to project naming.
func (m *Manager) GetStateWithProject(ctx context.Context, projectName, workspaceID string) (State, *ContainerInfo, error) {
	// First try project name if set (search both new and legacy labels)
	if projectName != "" {
		sanitized := docker.SanitizeProjectName(projectName)

		// Try new labels first
		containers, err := m.client.ListContainers(ctx, map[string]string{
			labels.LabelWorkspaceID: sanitized,
		})
		if err == nil && len(containers) > 0 {
			return m.processContainers(containers)
		}

		// Fall back to legacy labels
		containers, err = m.client.ListContainers(ctx, map[string]string{
			docker.LabelEnvKey: sanitized,
		})
		if err == nil && len(containers) > 0 {
			return m.processContainers(containers)
		}
	}

	// Fall back to workspace ID lookup
	return m.GetState(ctx, workspaceID)
}

// GetStateWithProjectAndHash combines project lookup with hash check.
func (m *Manager) GetStateWithProjectAndHash(ctx context.Context, projectName, envKey, currentConfigHash string) (State, *ContainerInfo, error) {
	state, info, err := m.GetStateWithProject(ctx, projectName, envKey)
	if err != nil || info == nil {
		return state, info, err
	}

	// Check if config has changed
	if info.ConfigHash != "" && info.ConfigHash != currentConfigHash {
		return StateStale, info, nil
	}

	return state, info, nil
}

// processContainers extracts state and info from a list of containers.
func (m *Manager) processContainers(containers []docker.Container) (State, *ContainerInfo, error) {
	if len(containers) == 0 {
		return StateAbsent, nil, nil
	}

	// Find the primary container (check both new and legacy labels)
	var primary *docker.Container
	for i := range containers {
		if isPrimaryContainer(containers[i].Labels) {
			primary = &containers[i]
			break
		}
	}

	// No primary container found - broken state
	if primary == nil {
		if len(containers) > 0 {
			info := containerInfoFromDocker(&containers[0])
			return StateBroken, info, nil
		}
		return StateBroken, nil, nil
	}

	info := containerInfoFromDocker(primary)

	if primary.Running {
		return StateRunning, info, nil
	}

	return StateCreated, info, nil
}

// FindContainers returns all containers for an environment.
func (m *Manager) FindContainers(ctx context.Context, workspaceID string) ([]ContainerInfo, error) {
	// Try new labels first
	containers, err := m.client.ListContainers(ctx, map[string]string{
		labels.LabelWorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}

	// Fall back to legacy labels
	if len(containers) == 0 {
		containers, err = m.client.ListContainers(ctx, map[string]string{
			docker.LabelEnvKey: workspaceID,
		})
		if err != nil {
			return nil, err
		}
	}

	result := make([]ContainerInfo, 0, len(containers))
	for i := range containers {
		result = append(result, *containerInfoFromDocker(&containers[i]))
	}

	return result, nil
}

// FindPrimaryContainer returns the primary container for an environment.
func (m *Manager) FindPrimaryContainer(ctx context.Context, workspaceID string) (*ContainerInfo, error) {
	// Try new labels first
	containers, err := m.client.ListContainers(ctx, map[string]string{
		labels.LabelWorkspaceID: workspaceID,
		labels.LabelIsPrimary:   "true",
	})
	if err != nil {
		return nil, err
	}

	// Fall back to legacy labels
	if len(containers) == 0 {
		containers, err = m.client.ListContainers(ctx, map[string]string{
			docker.LabelEnvKey:  workspaceID,
			docker.LabelPrimary: "true",
		})
		if err != nil {
			return nil, err
		}
	}

	if len(containers) == 0 {
		return nil, nil
	}

	return containerInfoFromDocker(&containers[0]), nil
}

// FindContainerByName returns a container by its name.
// This is used by the SSH command to find a specific container.
func (m *Manager) FindContainerByName(ctx context.Context, containerName string) (*ContainerInfo, error) {
	// Try new labels first
	containers, err := m.client.ListContainers(ctx, map[string]string{
		labels.LabelManaged: "true",
	})
	if err != nil {
		return nil, err
	}

	// Also include legacy-labeled containers
	legacyContainers, err := m.client.ListContainers(ctx, map[string]string{
		docker.LabelManaged: "true",
	})
	if err == nil {
		containers = append(containers, legacyContainers...)
	}

	for i := range containers {
		if containers[i].Name == containerName {
			return containerInfoFromDocker(&containers[i]), nil
		}
	}

	return nil, nil
}

// containerInfoFromDocker creates ContainerInfo from a Docker container.
// Handles both new labels and legacy labels.
func containerInfoFromDocker(c *docker.Container) *ContainerInfo {
	// Try to parse as new labels first
	l := labels.FromMap(c.Labels)

	// If new labels are empty, try legacy migration
	if l.WorkspaceID == "" && labels.IsLegacy(c.Labels) {
		l = labels.MigrateFromLegacy(c.Labels)
	}

	// Fall back to reading legacy labels directly if still empty
	if l.WorkspaceID == "" {
		legacyLabels := docker.LabelsFromMap(c.Labels)
		l.WorkspaceID = legacyLabels.EnvKey
		l.HashConfig = legacyLabels.ConfigHash
		l.IsPrimary = legacyLabels.Primary
		l.ComposeProject = legacyLabels.ComposeProject
		l.ComposeService = legacyLabels.PrimaryService
		l.BuildMethod = legacyLabels.Plan
	}

	return &ContainerInfo{
		ID:             c.ID,
		Name:           c.Name,
		Status:         c.Status,
		Running:        c.Running,
		ConfigHash:     l.HashConfig,
		WorkspaceID:    l.WorkspaceID,
		Plan:           l.BuildMethod,
		ComposeProject: l.ComposeProject,
		PrimaryService: l.ComposeService,
		Labels:         l,
	}
}

// isPrimaryContainer checks if a container is marked as primary (supports both label formats).
func isPrimaryContainer(labelMap map[string]string) bool {
	return labels.IsPrimaryContainer(labelMap)
}

// Cleanup removes all containers for an environment.
// This is useful for recovering from broken states.
// If removeVolumes is true, anonymous volumes attached to containers are also removed.
func (m *Manager) Cleanup(ctx context.Context, workspaceID string, removeVolumes bool) error {
	// Find containers with new labels
	containers, err := m.client.ListContainers(ctx, map[string]string{
		labels.LabelWorkspaceID: workspaceID,
	})
	if err != nil {
		return err
	}

	// Also find containers with legacy labels
	legacyContainers, err := m.client.ListContainers(ctx, map[string]string{
		docker.LabelEnvKey: workspaceID,
	})
	if err == nil {
		containers = append(containers, legacyContainers...)
	}

	var lastErr error
	for _, c := range containers {
		// Stop if running
		if c.Running {
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
func (m *Manager) ValidateState(ctx context.Context, envKey string, operation Operation) error {
	state, _, err := m.GetState(ctx, envKey)
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

// Operation represents a dcx operation.
type Operation string

const (
	OpStart Operation = "start"
	OpStop  Operation = "stop"
	OpExec  Operation = "exec"
	OpDown  Operation = "down"
	OpUp    Operation = "up"
)

// GetDiagnostics returns diagnostic information for troubleshooting.
func (m *Manager) GetDiagnostics(ctx context.Context, envKey string) (*Diagnostics, error) {
	state, info, err := m.GetState(ctx, envKey)
	if err != nil {
		return nil, err
	}

	containers, err := m.FindContainers(ctx, envKey)
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

// Diagnostics contains diagnostic information about an environment.
type Diagnostics struct {
	State            State
	Recovery         Recovery
	PrimaryContainer *ContainerInfo
	Containers       []ContainerInfo
}
