package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStateRequirementConstants(t *testing.T) {
	// Verify the constants have distinct values
	assert.NotEqual(t, RequireRunning, RequireExists)
	assert.NotEqual(t, RequireRunning, RequireAny)
	assert.NotEqual(t, RequireExists, RequireAny)

	// Verify the ordering (iota starts at 0)
	assert.Equal(t, StateRequirement(0), RequireRunning)
	assert.Equal(t, StateRequirement(1), RequireExists)
	assert.Equal(t, StateRequirement(2), RequireAny)
}

func TestStateValidationOptions(t *testing.T) {
	tests := []struct {
		name    string
		opts    StateValidationOptions
	}{
		{
			name: "require running with stale warning",
			opts: StateValidationOptions{
				Requirement: RequireRunning,
				WarnOnStale: true,
				AllowStale:  true,
			},
		},
		{
			name: "require running strict",
			opts: StateValidationOptions{
				Requirement: RequireRunning,
				WarnOnStale: false,
				AllowStale:  false,
			},
		},
		{
			name: "require exists",
			opts: StateValidationOptions{
				Requirement: RequireExists,
				WarnOnStale: true,
				AllowStale:  true,
			},
		},
		{
			name: "require any",
			opts: StateValidationOptions{
				Requirement: RequireAny,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just verify the struct can be created with various options
			assert.NotNil(t, tt.opts.Requirement)
		})
	}
}

func TestStateValidationResult(t *testing.T) {
	result := &StateValidationResult{
		Warnings: []string{"warning1", "warning2"},
	}

	assert.Len(t, result.Warnings, 2)
	assert.Contains(t, result.Warnings, "warning1")
	assert.Contains(t, result.Warnings, "warning2")
}

func TestStateValidationResultEmpty(t *testing.T) {
	result := &StateValidationResult{}

	assert.Nil(t, result.ContainerInfo)
	assert.Empty(t, result.Warnings)
}
