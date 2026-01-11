// Package state provides unified container state management types.
// This package replaces the previous internal/containerstate package
// with clearer naming (ContainerState instead of State).
package state

// ContainerState represents the lifecycle state of a container or environment.
// This is the canonical state type used throughout dcx.
// Renamed from State to ContainerState for clarity.
type ContainerState string

const (
	// StateUnknown represents an indeterminate or initial state.
	StateUnknown ContainerState = ""

	// StateAbsent means no managed containers exist for this environment.
	StateAbsent ContainerState = "absent"

	// StateCreated means containers exist but the primary is stopped.
	StateCreated ContainerState = "created"

	// StateRunning means the primary container is running.
	StateRunning ContainerState = "running"

	// StateStopped means the container was explicitly stopped.
	StateStopped ContainerState = "stopped"

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

// IsUsable returns true if the environment can be used (started/exec'd).
func (s ContainerState) IsUsable() bool {
	return s == StateCreated || s == StateRunning || s == StateStopped
}

// NeedsRecreate returns true if the environment needs to be recreated.
func (s ContainerState) NeedsRecreate() bool {
	return s == StateStale || s == StateBroken
}

// CanStart returns true if the environment can be started (without rebuild).
func (s ContainerState) CanStart() bool {
	return s == StateCreated || s == StateStopped
}

// CanStop returns true if the environment can be stopped.
func (s ContainerState) CanStop() bool {
	return s == StateRunning
}

// CanExec returns true if commands can be executed in the environment.
func (s ContainerState) CanExec() bool {
	return s == StateRunning
}

// IsAbsent returns true if no containers exist.
func (s ContainerState) IsAbsent() bool {
	return s == StateAbsent || s == StateUnknown
}

// Recovery represents a recovery action for a broken or stale state.
type Recovery struct {
	Action      RecoveryAction
	Description string
}

// RecoveryAction represents the type of recovery action.
type RecoveryAction string

const (
	// RecoveryRebuild indicates the environment should be rebuilt.
	RecoveryRebuild RecoveryAction = "rebuild"

	// RecoveryRemove indicates old containers should be removed.
	RecoveryRemove RecoveryAction = "remove"

	// RecoveryRestart indicates the container should be restarted.
	RecoveryRestart RecoveryAction = "restart"

	// RecoveryNone indicates no recovery is needed.
	RecoveryNone RecoveryAction = "none"
)

// GetRecovery returns the recommended recovery action for the current state.
func (s ContainerState) GetRecovery() Recovery {
	switch s {
	case StateAbsent, StateUnknown:
		return Recovery{
			Action:      RecoveryNone,
			Description: "No containers exist. Run 'dcx up' to create the environment.",
		}
	case StateCreated, StateStopped:
		return Recovery{
			Action:      RecoveryRestart,
			Description: "Container exists but is stopped. Run 'dcx start' to start it.",
		}
	case StateRunning:
		return Recovery{
			Action:      RecoveryNone,
			Description: "Environment is running normally.",
		}
	case StateStale:
		return Recovery{
			Action:      RecoveryRebuild,
			Description: "Configuration has changed. Run 'dcx up --rebuild' to apply changes.",
		}
	case StateBroken:
		return Recovery{
			Action:      RecoveryRemove,
			Description: "Environment is in an inconsistent state. Run 'dcx down' then 'dcx up' to recreate.",
		}
	default:
		return Recovery{
			Action:      RecoveryNone,
			Description: "Unknown state.",
		}
	}
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
			Changes: []string{"devcontainer.json modified"},
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
	case StateCreated, StateStopped:
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
	ConfigHash     string
	WorkspaceID    string // Stable identifier
	Plan           string
	ComposeProject string
	PrimaryService string
	Labels         *ContainerLabels
}

