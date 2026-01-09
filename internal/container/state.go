// Package container provides unified container state management types.
package container

// State represents the lifecycle state of a container or environment.
// This is the canonical state type used throughout dcx.
type State string

const (
	// StateUnknown represents an indeterminate or initial state.
	StateUnknown State = ""

	// StateAbsent means no managed containers exist for this environment.
	StateAbsent State = "absent"

	// StateCreated means containers exist but the primary is stopped.
	StateCreated State = "created"

	// StateRunning means the primary container is running.
	StateRunning State = "running"

	// StateStopped means the container was explicitly stopped.
	StateStopped State = "stopped"

	// StateStale means the primary container exists but its config hash
	// differs from the current computed hash.
	StateStale State = "stale"

	// StateBroken means managed containers exist but the primary is
	// missing or in an inconsistent state.
	StateBroken State = "broken"
)

// String returns the string representation of the state.
func (s State) String() string {
	if s == StateUnknown {
		return "unknown"
	}
	return string(s)
}

// IsUsable returns true if the environment can be used (started/exec'd).
func (s State) IsUsable() bool {
	return s == StateCreated || s == StateRunning || s == StateStopped
}

// NeedsRecreate returns true if the environment needs to be recreated.
func (s State) NeedsRecreate() bool {
	return s == StateStale || s == StateBroken
}

// CanStart returns true if the environment can be started (without rebuild).
func (s State) CanStart() bool {
	return s == StateCreated || s == StateStopped
}

// CanStop returns true if the environment can be stopped.
func (s State) CanStop() bool {
	return s == StateRunning
}

// CanExec returns true if commands can be executed in the environment.
func (s State) CanExec() bool {
	return s == StateRunning
}

// IsAbsent returns true if no containers exist.
func (s State) IsAbsent() bool {
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
func (s State) GetRecovery() Recovery {
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

// FromLegacyState converts the old uppercase state format to the new format.
// This is for backwards compatibility during migration.
func FromLegacyState(s string) State {
	switch s {
	case "ABSENT":
		return StateAbsent
	case "CREATED":
		return StateCreated
	case "RUNNING":
		return StateRunning
	case "STALE":
		return StateStale
	case "BROKEN":
		return StateBroken
	default:
		// Already in new format or unknown
		return State(s)
	}
}

// ToLegacyState converts the new state format to the old uppercase format.
// This is for backwards compatibility during migration.
func (s State) ToLegacyState() string {
	switch s {
	case StateAbsent:
		return "ABSENT"
	case StateCreated:
		return "CREATED"
	case StateRunning:
		return "RUNNING"
	case StateStale:
		return "STALE"
	case StateBroken:
		return "BROKEN"
	case StateStopped:
		return "CREATED" // Stopped maps to CREATED in legacy
	default:
		return string(s)
	}
}
