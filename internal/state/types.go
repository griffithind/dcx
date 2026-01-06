// Package state manages the lifecycle state of devcontainer environments.
package state

import "fmt"

// State represents the current state of a devcontainer environment.
type State string

const (
	// StateAbsent means no managed containers exist for this environment.
	StateAbsent State = "ABSENT"

	// StateCreated means containers exist but the primary is stopped.
	StateCreated State = "CREATED"

	// StateRunning means the primary container is running.
	StateRunning State = "RUNNING"

	// StateStale means the primary container exists but its config hash
	// differs from the current computed hash.
	StateStale State = "STALE"

	// StateBroken means managed containers exist but the primary is
	// missing or in an inconsistent state.
	StateBroken State = "BROKEN"
)

// String returns the string representation of the state.
func (s State) String() string {
	return string(s)
}

// IsUsable returns true if the environment can be used (started/exec'd).
func (s State) IsUsable() bool {
	return s == StateCreated || s == StateRunning
}

// NeedsRecreate returns true if the environment needs to be recreated.
func (s State) NeedsRecreate() bool {
	return s == StateStale || s == StateBroken
}

// CanStart returns true if the environment can be started (without rebuild).
func (s State) CanStart() bool {
	return s == StateCreated
}

// CanStop returns true if the environment can be stopped.
func (s State) CanStop() bool {
	return s == StateRunning
}

// CanExec returns true if commands can be executed in the environment.
func (s State) CanExec() bool {
	return s == StateRunning
}

// ContainerInfo holds information about a container relevant to state management.
type ContainerInfo struct {
	ID             string
	Name           string
	Status         string
	Running        bool
	ConfigHash     string
	EnvKey         string
	Plan           string
	ComposeProject string
	PrimaryService string
}

// StateError represents an error related to environment state.
type StateError struct {
	State   State
	Message string
	Err     error
}

func (e *StateError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s (state: %s): %v", e.Message, e.State, e.Err)
	}
	return fmt.Sprintf("%s (state: %s)", e.Message, e.State)
}

func (e *StateError) Unwrap() error {
	return e.Err
}

// NewStateError creates a new StateError.
func NewStateError(state State, message string, err error) *StateError {
	return &StateError{
		State:   state,
		Message: message,
		Err:     err,
	}
}

// ErrNotRunning indicates the container is not running when it should be.
var ErrNotRunning = NewStateError(StateCreated, "container is not running", nil)

// ErrAlreadyRunning indicates the container is already running.
var ErrAlreadyRunning = NewStateError(StateRunning, "container is already running", nil)

// ErrNoContainer indicates no container exists.
var ErrNoContainer = NewStateError(StateAbsent, "no container found for this environment", nil)

// ErrStaleConfig indicates the configuration has changed.
var ErrStaleConfig = NewStateError(StateStale, "configuration has changed, rebuild required", nil)

// ErrBrokenState indicates the environment is in an inconsistent state.
var ErrBrokenState = NewStateError(StateBroken, "environment is in an inconsistent state", nil)

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
	case StateAbsent:
		return Recovery{
			Action:      RecoveryNone,
			Description: "No containers exist. Run 'dcx up' to create the environment.",
		}
	case StateCreated:
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
