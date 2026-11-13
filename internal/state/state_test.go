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
