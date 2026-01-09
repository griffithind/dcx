package state

// This file contains state transition utilities.
// The core DeterminePlanAction function is in state.go.
// This file provides additional transition-related helpers.

// ValidTransitions defines which state transitions are valid.
var ValidTransitions = map[ContainerState][]ContainerState{
	StateAbsent:  {StateCreated},
	StateCreated: {StateRunning, StateAbsent},
	StateRunning: {StateStopped, StateStale, StateBroken, StateAbsent},
	StateStopped: {StateRunning, StateAbsent},
	StateStale:   {StateAbsent, StateCreated}, // Must recreate
	StateBroken:  {StateAbsent},               // Must remove
}

// CanTransition checks if a state transition is valid.
func CanTransition(from, to ContainerState) bool {
	validTo, ok := ValidTransitions[from]
	if !ok {
		return false
	}
	for _, s := range validTo {
		if s == to {
			return true
		}
	}
	return false
}

// RequiredActionForState returns the action needed to reach a target state.
func RequiredActionForState(current, target ContainerState) PlanAction {
	if current == target {
		return PlanActionNone
	}

	switch target {
	case StateRunning:
		switch current {
		case StateAbsent:
			return PlanActionCreate
		case StateCreated, StateStopped:
			return PlanActionStart
		case StateStale, StateBroken:
			return PlanActionRecreate
		}
	case StateAbsent:
		return PlanActionNone // Down operation handles this
	case StateStopped:
		if current == StateRunning {
			return PlanActionNone // Stop operation handles this
		}
	}

	return PlanActionNone
}

// IsTerminalState returns true if no further automatic transitions are expected.
func IsTerminalState(s ContainerState) bool {
	return s == StateRunning || s == StateAbsent
}

// NeedsUserIntervention returns true if the state requires user action to resolve.
func NeedsUserIntervention(s ContainerState) bool {
	return s == StateStale || s == StateBroken
}
