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
		{StateStopped, "stopped"},
		{StateStale, "stale"},
		{StateBroken, "broken"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestContainerStateHelpers(t *testing.T) {
	t.Run("IsUsable", func(t *testing.T) {
		assert.True(t, StateCreated.IsUsable())
		assert.True(t, StateRunning.IsUsable())
		assert.True(t, StateStopped.IsUsable())
		assert.False(t, StateAbsent.IsUsable())
		assert.False(t, StateStale.IsUsable())
		assert.False(t, StateBroken.IsUsable())
	})

	t.Run("NeedsRecreate", func(t *testing.T) {
		assert.True(t, StateStale.NeedsRecreate())
		assert.True(t, StateBroken.NeedsRecreate())
		assert.False(t, StateRunning.NeedsRecreate())
		assert.False(t, StateAbsent.NeedsRecreate())
	})

	t.Run("CanStart", func(t *testing.T) {
		assert.True(t, StateCreated.CanStart())
		assert.True(t, StateStopped.CanStart())
		assert.False(t, StateRunning.CanStart())
		assert.False(t, StateAbsent.CanStart())
	})

	t.Run("CanStop", func(t *testing.T) {
		assert.True(t, StateRunning.CanStop())
		assert.False(t, StateStopped.CanStop())
		assert.False(t, StateAbsent.CanStop())
	})

	t.Run("CanExec", func(t *testing.T) {
		assert.True(t, StateRunning.CanExec())
		assert.False(t, StateStopped.CanExec())
		assert.False(t, StateCreated.CanExec())
	})

	t.Run("IsAbsent", func(t *testing.T) {
		assert.True(t, StateAbsent.IsAbsent())
		assert.True(t, StateUnknown.IsAbsent())
		assert.False(t, StateRunning.IsAbsent())
	})
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
			name:     "stopped starts",
			state:    StateStopped,
			expected: PlanActionStart,
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

func TestGetRecovery(t *testing.T) {
	t.Run("absent state", func(t *testing.T) {
		r := StateAbsent.GetRecovery()
		assert.Equal(t, RecoveryNone, r.Action)
	})

	t.Run("stopped state", func(t *testing.T) {
		r := StateStopped.GetRecovery()
		assert.Equal(t, RecoveryRestart, r.Action)
	})

	t.Run("running state", func(t *testing.T) {
		r := StateRunning.GetRecovery()
		assert.Equal(t, RecoveryNone, r.Action)
	})

	t.Run("stale state", func(t *testing.T) {
		r := StateStale.GetRecovery()
		assert.Equal(t, RecoveryRebuild, r.Action)
	})

	t.Run("broken state", func(t *testing.T) {
		r := StateBroken.GetRecovery()
		assert.Equal(t, RecoveryRemove, r.Action)
	})
}
