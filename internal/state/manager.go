package state

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"strings"

	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/util"
)

// Manager handles state detection and management for devcontainer environments.
type Manager struct {
	client *docker.Client
}

// NewManager creates a new state manager.
func NewManager(client *docker.Client) *Manager {
	return &Manager{client: client}
}

// SanitizeProjectName ensures the name is valid for Docker container/compose project names.
// Docker requires lowercase alphanumeric with hyphens/underscores, starting with letter.
func SanitizeProjectName(name string) string {
	if name == "" {
		return ""
	}

	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace spaces with underscores and filter invalid characters
	var result strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			result.WriteRune(r)
		} else if r == ' ' {
			result.WriteRune('_')
		}
		// Skip other characters
	}

	sanitized := result.String()
	if sanitized == "" {
		return ""
	}

	// Ensure starts with a letter (Docker requirement)
	if sanitized[0] >= '0' && sanitized[0] <= '9' {
		sanitized = "dcx_" + sanitized
	}

	return sanitized
}

// ResolveIdentifier returns the project name if configured, otherwise computes env_key.
// The projectName parameter is from dcx.json configuration.
func ResolveIdentifier(workspacePath string, projectName string) string {
	if projectName != "" {
		return SanitizeProjectName(projectName)
	}
	return ComputeEnvKey(workspacePath)
}

// ComputeEnvKey generates a stable identifier from the workspace path.
// Returns base32(sha256(realpath(workspace_root)))[0:12]
func ComputeEnvKey(workspacePath string) string {
	// Get the real path (resolve symlinks)
	realPath, err := util.RealPath(workspacePath)
	if err != nil {
		// Fall back to the original path if we can't resolve
		realPath = workspacePath
	}

	// Normalize the path
	realPath = util.NormalizePath(realPath)

	// Compute SHA256
	hash := sha256.Sum256([]byte(realPath))

	// Encode as base32 and take first 12 characters
	encoded := base32.StdEncoding.EncodeToString(hash[:])
	encoded = strings.ToLower(encoded)

	if len(encoded) > 12 {
		encoded = encoded[:12]
	}

	return encoded
}

// ComputeWorkspaceHash computes the full hash of the workspace path.
func ComputeWorkspaceHash(workspacePath string) string {
	realPath, err := util.RealPath(workspacePath)
	if err != nil {
		realPath = workspacePath
	}
	realPath = util.NormalizePath(realPath)

	hash := sha256.Sum256([]byte(realPath))
	return base32.StdEncoding.EncodeToString(hash[:])
}

// GetState determines the current state of the devcontainer environment.
func (m *Manager) GetState(ctx context.Context, envKey string) (State, *ContainerInfo, error) {
	// Find all containers with our env_key
	containers, err := m.client.ListContainers(ctx, map[string]string{
		docker.LabelEnvKey: envKey,
	})
	if err != nil {
		return StateAbsent, nil, err
	}

	// No containers found
	if len(containers) == 0 {
		return StateAbsent, nil, nil
	}

	// Find the primary container
	var primary *docker.Container
	for i := range containers {
		labels := docker.LabelsFromMap(containers[i].Labels)
		if labels.Primary {
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

// GetStateWithProject handles lookup for both project-named and env_key containers.
// This enables migration from hash-based naming to project naming.
func (m *Manager) GetStateWithProject(ctx context.Context, projectName, envKey string) (State, *ContainerInfo, error) {
	// First try project name if set
	if projectName != "" {
		sanitized := SanitizeProjectName(projectName)
		containers, err := m.client.ListContainers(ctx, map[string]string{
			docker.LabelEnvKey: sanitized,
		})
		if err == nil && len(containers) > 0 {
			return m.processContainers(containers)
		}
	}

	// Fall back to env_key lookup (for migration)
	return m.GetState(ctx, envKey)
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

	// Find the primary container
	var primary *docker.Container
	for i := range containers {
		labels := docker.LabelsFromMap(containers[i].Labels)
		if labels.Primary {
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
func (m *Manager) FindContainers(ctx context.Context, envKey string) ([]ContainerInfo, error) {
	containers, err := m.client.ListContainers(ctx, map[string]string{
		docker.LabelEnvKey: envKey,
	})
	if err != nil {
		return nil, err
	}

	result := make([]ContainerInfo, 0, len(containers))
	for i := range containers {
		result = append(result, *containerInfoFromDocker(&containers[i]))
	}

	return result, nil
}

// FindPrimaryContainer returns the primary container for an environment.
func (m *Manager) FindPrimaryContainer(ctx context.Context, envKey string) (*ContainerInfo, error) {
	containers, err := m.client.ListContainers(ctx, map[string]string{
		docker.LabelEnvKey:  envKey,
		docker.LabelPrimary: "true",
	})
	if err != nil {
		return nil, err
	}

	if len(containers) == 0 {
		return nil, nil
	}

	return containerInfoFromDocker(&containers[0]), nil
}

// FindContainerByName returns a container by its name.
// This is used by the SSH command to find a specific container.
func (m *Manager) FindContainerByName(ctx context.Context, containerName string) (*ContainerInfo, error) {
	containers, err := m.client.ListContainers(ctx, map[string]string{
		docker.LabelManaged: "true",
	})
	if err != nil {
		return nil, err
	}

	for i := range containers {
		if containers[i].Name == containerName {
			return containerInfoFromDocker(&containers[i]), nil
		}
	}

	return nil, nil
}

func containerInfoFromDocker(c *docker.Container) *ContainerInfo {
	labels := docker.LabelsFromMap(c.Labels)
	return &ContainerInfo{
		ID:             c.ID,
		Name:           c.Name,
		Status:         c.Status,
		Running:        c.Running,
		ConfigHash:     labels.ConfigHash,
		EnvKey:         labels.EnvKey,
		Plan:           labels.Plan,
		ComposeProject: labels.ComposeProject,
		PrimaryService: labels.PrimaryService,
		Labels:         labels,
	}
}

// Cleanup removes all containers for an environment.
// This is useful for recovering from broken states.
// If removeVolumes is true, anonymous volumes attached to containers are also removed.
func (m *Manager) Cleanup(ctx context.Context, envKey string, removeVolumes bool) error {
	containers, err := m.client.ListContainers(ctx, map[string]string{
		docker.LabelEnvKey: envKey,
	})
	if err != nil {
		return err
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
