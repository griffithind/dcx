package state

import (
	"context"
	"time"
)

// ContainerClient is what the state manager needs from the container
// engine: listing containers by label. Keeping it this small avoids a
// dependency on the container package.
type ContainerClient interface {
	ListContainersWithLabels(ctx context.Context, labels map[string]string) ([]ContainerSummary, error)
}

// ContainerSummary is a minimal container summary returned by ListContainersWithLabels.
type ContainerSummary struct {
	ID      string
	Name    string
	State   string
	Running bool
	Labels  map[string]string
	Created time.Time
	Env     []string // Config.Env
	User    string   // Config.User
}

// ContainerDetails is detailed container info returned by InspectContainer.
type ContainerDetails struct {
	ID        string
	Name      string
	State     string
	Running   bool
	StartedAt string
	Image     string
	Labels    map[string]string
	Mounts    []string
	// Volumes are the named volumes mounted in the container.
	Volumes    []string
	WorkingDir string
	// Env is the container's environment (image ENV plus containerEnv) as
	// KEY=VALUE pairs.
	Env []string
}

// StateManager detects the state of a workspace's containers from their labels.
type StateManager struct {
	client ContainerClient
}

// NewStateManager creates a new state manager.
func NewStateManager(client ContainerClient) *StateManager {
	return &StateManager{client: client}
}

// State determines the current state of the devcontainer environment.
func (m *StateManager) State(ctx context.Context, workspaceID string) (ContainerState, *ContainerInfo, error) {
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
		if containers[i].Labels[LabelIsPrimary] == "true" {
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
		Env:            c.Env,
		User:           c.User,
	}
}

// CurrentHashVersion is stamped on containers as LabelHashVersion.
//
// "3": the staleness key covers devcontainer.json as written (before
// substitution) and no feature digests; those are in hash.inputs.
const CurrentHashVersion = "3"

// IsLegacyHash reports whether the container's config hash was computed by
// an older dcx and therefore cannot be used to detect config changes.
func (c *ContainerInfo) IsLegacyHash() bool {
	return c != nil && c.HashVersion != CurrentHashVersion
}

// GetStateWithProjectAndHash combines project lookup with hash check.
func (m *StateManager) StateWithHash(ctx context.Context, workspaceID, currentConfigHash string) (ContainerState, *ContainerInfo, error) {
	state, info, err := m.State(ctx, workspaceID)
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
