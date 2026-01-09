package container

import (
	"testing"
)

func TestState_String(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateUnknown, "unknown"},
		{StateAbsent, "absent"},
		{StateCreated, "created"},
		{StateRunning, "running"},
		{StateStopped, "stopped"},
		{StateStale, "stale"},
		{StateBroken, "broken"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("State.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestState_IsUsable(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateUnknown, false},
		{StateAbsent, false},
		{StateCreated, true},
		{StateRunning, true},
		{StateStopped, true},
		{StateStale, false},
		{StateBroken, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.IsUsable(); got != tt.expected {
				t.Errorf("State.IsUsable() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestState_NeedsRecreate(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateUnknown, false},
		{StateAbsent, false},
		{StateCreated, false},
		{StateRunning, false},
		{StateStopped, false},
		{StateStale, true},
		{StateBroken, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.NeedsRecreate(); got != tt.expected {
				t.Errorf("State.NeedsRecreate() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestState_CanStart(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateCreated, true},
		{StateStopped, true},
		{StateRunning, false},
		{StateAbsent, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.CanStart(); got != tt.expected {
				t.Errorf("State.CanStart() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestState_CanExec(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateRunning, true},
		{StateCreated, false},
		{StateStopped, false},
		{StateAbsent, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.CanExec(); got != tt.expected {
				t.Errorf("State.CanExec() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestFromLegacyState(t *testing.T) {
	tests := []struct {
		input    string
		expected State
	}{
		{"ABSENT", StateAbsent},
		{"CREATED", StateCreated},
		{"RUNNING", StateRunning},
		{"STALE", StateStale},
		{"BROKEN", StateBroken},
		{"absent", StateAbsent},
		{"running", StateRunning},
		{"unknown", State("unknown")},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := FromLegacyState(tt.input); got != tt.expected {
				t.Errorf("FromLegacyState(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestState_ToLegacyState(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateAbsent, "ABSENT"},
		{StateCreated, "CREATED"},
		{StateRunning, "RUNNING"},
		{StateStale, "STALE"},
		{StateBroken, "BROKEN"},
		{StateStopped, "CREATED"}, // Stopped maps to CREATED
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			if got := tt.state.ToLegacyState(); got != tt.expected {
				t.Errorf("State.ToLegacyState() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestState_GetRecovery(t *testing.T) {
	tests := []struct {
		state          State
		expectedAction RecoveryAction
	}{
		{StateAbsent, RecoveryNone},
		{StateUnknown, RecoveryNone},
		{StateCreated, RecoveryRestart},
		{StateStopped, RecoveryRestart},
		{StateRunning, RecoveryNone},
		{StateStale, RecoveryRebuild},
		{StateBroken, RecoveryRemove},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			recovery := tt.state.GetRecovery()
			if recovery.Action != tt.expectedAction {
				t.Errorf("State.GetRecovery().Action = %q, want %q", recovery.Action, tt.expectedAction)
			}
		})
	}
}
