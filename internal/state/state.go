// Package state detects the state of a workspace's containers from their
// Docker labels and defines the label schema.
package state

import "path/filepath"

// ContainerState is the lifecycle state of a workspace's containers.
type ContainerState string

const (
	// StateUnknown represents an indeterminate or initial state.
	StateUnknown ContainerState = ""

	// StateAbsent means no managed containers exist for this environment.
	StateAbsent ContainerState = "absent"

	// StateCreated means containers exist but the primary is not running
	// (created, or stopped).
	StateCreated ContainerState = "created"

	// StateRunning means the primary container is running.
	StateRunning ContainerState = "running"

	// StateStale means the primary container exists but its config hash
	// differs from the current computed hash.
	StateStale ContainerState = "stale"

	// StateBroken means managed containers exist but the primary is
	// missing or in an inconsistent state.
	StateBroken ContainerState = "broken"
)

// String returns the string representation of the state.
func (s ContainerState) String() string {
	if s == StateUnknown {
		return "unknown"
	}
	return string(s)
}

// PlanAction represents the action to be taken for an environment.
type PlanAction string

const (
	// PlanActionNone means no action is needed.
	PlanActionNone PlanAction = "none"

	// PlanActionStart means the container should be started.
	PlanActionStart PlanAction = "start"

	// PlanActionCreate means the environment should be created.
	PlanActionCreate PlanAction = "create"

	// PlanActionRecreate means the environment should be removed and recreated.
	PlanActionRecreate PlanAction = "recreate"

	// PlanActionRebuild means the environment should be rebuilt with new images.
	PlanActionRebuild PlanAction = "rebuild"
)

// PlanActionResult contains the result of determining what action to take.
type PlanActionResult struct {
	Action  PlanAction
	Reason  string
	Changes []string
}

// DeterminePlanAction determines what action should be taken based on current state
// and user options. This is the single source of truth for action decisions.
func DeterminePlanAction(state ContainerState, rebuild, recreate bool) PlanActionResult {
	switch state {
	case StateRunning:
		if rebuild {
			return PlanActionResult{
				Action: PlanActionRebuild,
				Reason: "force rebuild requested",
			}
		}
		if recreate {
			return PlanActionResult{
				Action: PlanActionRecreate,
				Reason: "force recreate requested",
			}
		}
		return PlanActionResult{
			Action: PlanActionNone,
			Reason: "container is running and up to date",
		}
	case StateStale:
		return PlanActionResult{
			Action:  PlanActionRecreate,
			Reason:  "configuration changed",
			Changes: []string{"configuration or build inputs modified"},
		}
	case StateBroken:
		return PlanActionResult{
			Action: PlanActionRecreate,
			Reason: "container state is broken",
		}
	case StateAbsent:
		return PlanActionResult{
			Action: PlanActionCreate,
			Reason: "no container found",
		}
	case StateCreated:
		return PlanActionResult{
			Action: PlanActionStart,
			Reason: "container exists but stopped",
		}
	default:
		return PlanActionResult{
			Action: PlanActionCreate,
			Reason: "unknown state",
		}
	}
}

// ContainerInfo holds information about a container relevant to state management.
type ContainerInfo struct {
	ID             string
	Name           string
	Status         string
	Running        bool
	ConfigHash     string // Staleness key of the config the container was created from
	HashVersion    string // Algorithm version of ConfigHash ("" = legacy)
	WorkspaceID    string // Stable identifier
	Plan           string
	ComposeProject string
	PrimaryService string
	Labels         *ContainerLabels
}

// IsSingleContainer returns true if this is a single-container (non-compose) devcontainer.
func (c *ContainerInfo) IsSingleContainer() bool {
	return c != nil && (c.Plan == BuildMethodImage || c.Plan == BuildMethodDockerfile)
}

// GetComposeProject returns the compose project name, falling back to the provided default.
func (c *ContainerInfo) GetComposeProject(defaultProject string) string {
	if c != nil && c.ComposeProject != "" {
		return c.ComposeProject
	}
	return defaultProject
}

// GetConfigDir returns the config directory from labels, falling back to the provided default.
func (c *ContainerInfo) GetConfigDir(defaultDir string) string {
	if c != nil && c.Labels != nil && c.Labels.ConfigPath != "" {
		return filepath.Dir(c.Labels.ConfigPath)
	}
	return defaultDir
}
