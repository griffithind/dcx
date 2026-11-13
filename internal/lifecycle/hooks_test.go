package lifecycle

import (
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
)

func TestWaitForOrder(t *testing.T) {
	// Verify the order of lifecycle commands
	assert.Less(t, waitForOrder[WaitForInitializeCommand], waitForOrder[WaitForOnCreateCommand])
	assert.Less(t, waitForOrder[WaitForOnCreateCommand], waitForOrder[WaitForUpdateContentCommand])
	assert.Less(t, waitForOrder[WaitForUpdateContentCommand], waitForOrder[WaitForPostCreateCommand])
	assert.Less(t, waitForOrder[WaitForPostCreateCommand], waitForOrder[WaitForPostStartCommand])
}

func TestShouldBlock(t *testing.T) {
	tests := []struct {
		name     string
		waitFor  string
		hookType WaitFor
		expected bool
	}{
		// Default per spec is updateContentCommand - initialize, onCreate, updateContent block
		{"default blocks initialize", "", WaitForInitializeCommand, true},
		{"default blocks onCreate", "", WaitForOnCreateCommand, true},
		{"default blocks updateContent", "", WaitForUpdateContentCommand, true},
		{"default doesn't block postCreate", "", WaitForPostCreateCommand, false},
		{"default doesn't block postStart", "", WaitForPostStartCommand, false},

		// waitFor: onCreateCommand - only initialize and onCreate should block
		{"onCreate blocks initialize", "onCreateCommand", WaitForInitializeCommand, true},
		{"onCreate blocks onCreate", "onCreateCommand", WaitForOnCreateCommand, true},
		{"onCreate doesn't block updateContent", "onCreateCommand", WaitForUpdateContentCommand, false},
		{"onCreate doesn't block postCreate", "onCreateCommand", WaitForPostCreateCommand, false},
		{"onCreate doesn't block postStart", "onCreateCommand", WaitForPostStartCommand, false},

		// waitFor: initializeCommand - only initialize should block
		{"initialize blocks initialize", "initializeCommand", WaitForInitializeCommand, true},
		{"initialize doesn't block onCreate", "initializeCommand", WaitForOnCreateCommand, false},

		// Invalid waitFor value should default to updateContentCommand per spec
		{"invalid defaults to updateContent", "invalidValue", WaitForUpdateContentCommand, true},
		{"invalid doesn't block postCreate", "invalidValue", WaitForPostCreateCommand, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &HookRunner{hooks: devcontainer.Hooks{WaitFor: tt.waitFor}}
			result := runner.shouldBlock(tt.hookType)
			assert.Equal(t, tt.expected, result, "shouldBlock(%s) with waitFor=%s", tt.hookType, tt.waitFor)
		})
	}
}

func TestGetWaitFor(t *testing.T) {
	tests := []struct {
		name     string
		waitFor  string
		expected WaitFor
	}{
		// Per spec, default is updateContentCommand
		{"empty defaults to updateContentCommand", "", WaitForUpdateContentCommand},
		{"valid onCreateCommand", "onCreateCommand", WaitForOnCreateCommand},
		{"valid updateContentCommand", "updateContentCommand", WaitForUpdateContentCommand},
		{"valid postCreateCommand", "postCreateCommand", WaitForPostCreateCommand},
		{"valid postStartCommand", "postStartCommand", WaitForPostStartCommand},
		{"invalid defaults to updateContentCommand", "invalidValue", WaitForUpdateContentCommand},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &HookRunner{hooks: devcontainer.Hooks{WaitFor: tt.waitFor}}
			result := runner.getWaitFor()
			assert.Equal(t, tt.expected, result)
		})
	}
}
