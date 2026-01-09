// Package container provides unified container state management types.
package container

import (
	"fmt"

	"github.com/griffithind/dcx/internal/labels"
)

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

// ContainerInfo holds information about a container relevant to state management.
type ContainerInfo struct {
	ID             string
	Name           string
	Status         string
	Running        bool
	ConfigHash     string
	WorkspaceID    string // Stable identifier (replaces WorkspaceID)
	Plan           string
	ComposeProject string
	PrimaryService string
	Labels         *labels.Labels
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

// Operation represents a dcx operation.
type Operation string

const (
	OpStart Operation = "start"
	OpStop  Operation = "stop"
	OpExec  Operation = "exec"
	OpDown  Operation = "down"
	OpUp    Operation = "up"
)

// Diagnostics contains diagnostic information about an environment.
type Diagnostics struct {
	State            State
	Recovery         Recovery
	PrimaryContainer *ContainerInfo
	Containers       []ContainerInfo
}
