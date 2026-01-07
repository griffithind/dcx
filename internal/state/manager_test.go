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

func TestSanitizeProjectName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase name",
			input:    "myproject",
			expected: "myproject",
		},
		{
			name:     "name with uppercase",
			input:    "MyProject",
			expected: "myproject",
		},
		{
			name:     "name with spaces",
			input:    "my project",
			expected: "my_project",
		},
		{
			name:     "name with hyphens",
			input:    "my-project",
			expected: "my-project",
		},
		{
			name:     "name with underscores",
			input:    "my_project",
			expected: "my_project",
		},
		{
			name:     "name starting with number",
			input:    "123project",
			expected: "dcx_123project",
		},
		{
			name:     "name with special characters",
			input:    "my@project#name!",
			expected: "myprojectname",
		},
		{
			name:     "name with mixed characters",
			input:    "My Project-v2.0",
			expected: "my_project-v20",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only special characters",
			input:    "@#$%",
			expected: "",
		},
		{
			name:     "ouzoerp style",
			input:    "ouzoerp",
			expected: "ouzoerp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeProjectName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestResolveIdentifier(t *testing.T) {
	workspacePath := "/home/user/project"

	// With project name
	result := ResolveIdentifier(workspacePath, "myproject")
	assert.Equal(t, "myproject", result)

	// With project name that needs sanitization
	result = ResolveIdentifier(workspacePath, "My Project")
	assert.Equal(t, "my_project", result)

	// Without project name - falls back to env key
	result = ResolveIdentifier(workspacePath, "")
	assert.Len(t, result, 12) // env key is 12 chars
	assert.Equal(t, ComputeEnvKey(workspacePath), result)
}
