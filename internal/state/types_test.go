package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStateCanStart(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateAbsent, false},
		{StateCreated, true},
		{StateRunning, false},
		{StateStale, false},
		{StateBroken, false},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.CanStart())
		})
	}
}

func TestStateCanStop(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateAbsent, false},
		{StateCreated, false},
		{StateRunning, true},
		{StateStale, false},
		{StateBroken, false},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.CanStop())
		})
	}
}

func TestStateCanExec(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateAbsent, false},
		{StateCreated, false},
		{StateRunning, true},
		{StateStale, false},
		{StateBroken, false},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.CanExec())
		})
	}
}

func TestStateGetRecovery(t *testing.T) {
	tests := []struct {
		state          State
		expectedAction RecoveryAction
	}{
		{StateAbsent, RecoveryNone},
		{StateCreated, RecoveryRestart},
		{StateRunning, RecoveryNone},
		{StateStale, RecoveryRebuild},
		{StateBroken, RecoveryRemove},
	}

	for _, tt := range tests {
		t.Run(tt.state.String(), func(t *testing.T) {
			recovery := tt.state.GetRecovery()
			assert.Equal(t, tt.expectedAction, recovery.Action)
			assert.NotEmpty(t, recovery.Description)
		})
	}
}

func TestStateError(t *testing.T) {
	t.Run("with underlying error", func(t *testing.T) {
		err := NewStateError(StateStale, "config changed", assert.AnError)
		assert.Contains(t, err.Error(), "config changed")
		assert.Contains(t, err.Error(), "STALE")
		assert.Equal(t, assert.AnError, err.Unwrap())
	})

	t.Run("without underlying error", func(t *testing.T) {
		err := NewStateError(StateBroken, "no primary container", nil)
		assert.Contains(t, err.Error(), "no primary container")
		assert.Contains(t, err.Error(), "BROKEN")
		assert.Nil(t, err.Unwrap())
	})
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name  string
		err   *StateError
		state State
	}{
		{"ErrNotRunning", ErrNotRunning, StateCreated},
		{"ErrAlreadyRunning", ErrAlreadyRunning, StateRunning},
		{"ErrNoContainer", ErrNoContainer, StateAbsent},
		{"ErrStaleConfig", ErrStaleConfig, StateStale},
		{"ErrBrokenState", ErrBrokenState, StateBroken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.state, tt.err.State)
			assert.NotEmpty(t, tt.err.Message)
		})
	}
}
