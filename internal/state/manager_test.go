package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeEnvKey(t *testing.T) {
	tests := []struct {
		name          string
		workspacePath string
	}{
		{
			name:          "simple path",
			workspacePath: "/home/user/project",
		},
		{
			name:          "path with spaces",
			workspacePath: "/home/user/my project",
		},
		{
			name:          "nested path",
			workspacePath: "/home/user/projects/myapp/src",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := ComputeEnvKey(tt.workspacePath)

			// Should be 12 characters
			assert.Len(t, key, 12)

			// Should be lowercase
			assert.Equal(t, key, key)

			// Should be deterministic
			key2 := ComputeEnvKey(tt.workspacePath)
			assert.Equal(t, key, key2)
		})
	}
}

func TestComputeEnvKeyDifferentPaths(t *testing.T) {
	key1 := ComputeEnvKey("/home/user/project1")
	key2 := ComputeEnvKey("/home/user/project2")

	// Different paths should produce different keys
	assert.NotEqual(t, key1, key2)
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state    State
		expected string
	}{
		{StateAbsent, "ABSENT"},
		{StateCreated, "CREATED"},
		{StateRunning, "RUNNING"},
		{StateStale, "STALE"},
		{StateBroken, "BROKEN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.String())
		})
	}
}

func TestStateIsUsable(t *testing.T) {
	usable := []State{StateCreated, StateRunning}
	notUsable := []State{StateAbsent, StateStale, StateBroken}

	for _, s := range usable {
		assert.True(t, s.IsUsable(), "expected %s to be usable", s)
	}

	for _, s := range notUsable {
		assert.False(t, s.IsUsable(), "expected %s to not be usable", s)
	}
}

func TestStateNeedsRecreate(t *testing.T) {
	needsRecreate := []State{StateStale, StateBroken}
	noRecreate := []State{StateAbsent, StateCreated, StateRunning}

	for _, s := range needsRecreate {
		assert.True(t, s.NeedsRecreate(), "expected %s to need recreate", s)
	}

	for _, s := range noRecreate {
		assert.False(t, s.NeedsRecreate(), "expected %s to not need recreate", s)
	}
}

func TestComputeWorkspaceHash(t *testing.T) {
	path := "/home/user/project"
	hash := ComputeWorkspaceHash(path)

	// Should be non-empty
	assert.NotEmpty(t, hash)

	// Should be deterministic
	hash2 := ComputeWorkspaceHash(path)
	assert.Equal(t, hash, hash2)

	// Different paths should produce different hashes
	hash3 := ComputeWorkspaceHash("/home/user/other")
	assert.NotEqual(t, hash, hash3)
}
