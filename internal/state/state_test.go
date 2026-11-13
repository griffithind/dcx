package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainerStateString(t *testing.T) {
	tests := []struct {
		state    ContainerState
		expected string
	}{
		{StateUnknown, "unknown"},
		{StateAbsent, "absent"},
		{StateRunning, "running"},
		{StateCreated, "created"},
		{StateStale, "stale"},
		{StateBroken, "broken"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestDeterminePlanAction(t *testing.T) {
	tests := []struct {
		name     string
		state    ContainerState
		rebuild  bool
		recreate bool
		expected PlanAction
	}{
		{
			name:     "running with no flags",
			state:    StateRunning,
			expected: PlanActionNone,
		},
		{
			name:     "running with rebuild flag",
			state:    StateRunning,
			rebuild:  true,
			expected: PlanActionRebuild,
		},
		{
			name:     "running with recreate flag",
			state:    StateRunning,
			recreate: true,
			expected: PlanActionRecreate,
		},
		{
			name:     "stale always recreates",
			state:    StateStale,
			expected: PlanActionRecreate,
		},
		{
			name:     "broken always recreates",
			state:    StateBroken,
			expected: PlanActionRecreate,
		},
		{
			name:     "absent creates",
			state:    StateAbsent,
			expected: PlanActionCreate,
		},
		{
			name:     "created starts",
			state:    StateCreated,
			expected: PlanActionStart,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DeterminePlanAction(tt.state, tt.rebuild, tt.recreate)
			assert.Equal(t, tt.expected, result.Action)
		})
	}
}
