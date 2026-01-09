package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
)

// StateRequirement specifies what container state is required for a command.
type StateRequirement int

const (
	// RequireRunning means the container must be running (for exec, shell, run).
	RequireRunning StateRequirement = iota

	// RequireExists means the container must exist in any state (for logs, stop).
	RequireExists

	// RequireAny means any state is acceptable, including absent (for up, down, status).
	RequireAny
)

// StateValidationOptions configures state validation behavior.
type StateValidationOptions struct {
	// Requirement specifies what state is required.
	Requirement StateRequirement

	// WarnOnStale shows a warning if the container is stale but continues.
	WarnOnStale bool

	// AllowStale permits stale containers without error.
	AllowStale bool
}

// StateValidationResult contains the validation outcome.
type StateValidationResult struct {
	// State is the current container state.
	State state.State

	// ContainerInfo is the container metadata, may be nil if absent.
	ContainerInfo *state.ContainerInfo

	// Warnings collected during validation.
	Warnings []string
}

// ValidateState checks if the current state meets the specified requirements.
// Returns an error if requirements are not met.
func ValidateState(cliCtx *CLIContext, opts StateValidationOptions) (*StateValidationResult, error) {
	currentState, containerInfo, err := cliCtx.GetState()
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %w", err)
	}

	result := &StateValidationResult{
		State:         currentState,
		ContainerInfo: containerInfo,
	}

	switch opts.Requirement {
	case RequireRunning:
		switch currentState {
		case state.StateAbsent:
			return nil, fmt.Errorf("no devcontainer found; run 'dcx up' first")
		case state.StateCreated:
			return nil, fmt.Errorf("devcontainer is not running; run 'dcx start' first")
		case state.StateBroken:
			return nil, fmt.Errorf("devcontainer is in broken state; run 'dcx up --recreate'")
		case state.StateStale:
			if opts.WarnOnStale {
				result.Warnings = append(result.Warnings, "devcontainer is stale (config changed)")
			}
			if !opts.AllowStale {
				return nil, fmt.Errorf("devcontainer is stale; run 'dcx up' to update")
			}
		case state.StateRunning:
			// Good - container is running
		}

		// Ensure we have container info
		if containerInfo == nil {
			return nil, fmt.Errorf("no primary container found")
		}

	case RequireExists:
		if currentState == state.StateAbsent {
			return nil, fmt.Errorf("no devcontainer found; run 'dcx up' first")
		}
		if containerInfo == nil {
			return nil, fmt.Errorf("no container found")
		}

	case RequireAny:
		// Any state is acceptable
	}

	// Print warnings
	for _, w := range result.Warnings {
		ui.Warning(w)
	}

	return result, nil
}

// RequireRunningContainer is a convenience function for exec-like commands.
// It validates the container is running and returns the container info.
func RequireRunningContainer(cliCtx *CLIContext) (*state.ContainerInfo, error) {
	result, err := ValidateState(cliCtx, StateValidationOptions{
		Requirement: RequireRunning,
		WarnOnStale: true,
		AllowStale:  true, // Allow stale with warning for exec
	})
	if err != nil {
		return nil, err
	}
	return result.ContainerInfo, nil
}

// RequireExistingContainer is a convenience function for commands that need
// a container to exist but don't require it to be running.
func RequireExistingContainer(cliCtx *CLIContext) (*state.ContainerInfo, error) {
	result, err := ValidateState(cliCtx, StateValidationOptions{
		Requirement: RequireExists,
		WarnOnStale: true,
		AllowStale:  true,
	})
	if err != nil {
		return nil, err
	}
	return result.ContainerInfo, nil
}

// CheckState returns the current state without enforcing requirements.
// Useful for commands like 'status' that just want to display state.
func CheckState(cliCtx *CLIContext) (*StateValidationResult, error) {
	return ValidateState(cliCtx, StateValidationOptions{
		Requirement: RequireAny,
	})
}
