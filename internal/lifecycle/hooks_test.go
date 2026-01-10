package lifecycle

import (
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCommand_String(t *testing.T) {
	result := parseCommand("echo hello")
	require.Len(t, result, 1)
	assert.Equal(t, []string{"echo hello"}, result[0].Args)
	assert.True(t, result[0].UseShell, "string commands should use shell")
}

func TestParseCommand_StringArray(t *testing.T) {
	result := parseCommand([]string{"echo", "hello", "world"})
	require.Len(t, result, 1)
	assert.Equal(t, []string{"echo", "hello", "world"}, result[0].Args)
	assert.False(t, result[0].UseShell, "array commands should not use shell")
}

func TestParseCommand_InterfaceArray(t *testing.T) {
	result := parseCommand([]interface{}{"echo", "hello"})
	require.Len(t, result, 1)
	assert.Equal(t, []string{"echo", "hello"}, result[0].Args)
	assert.False(t, result[0].UseShell, "array commands should not use shell")
}

func TestParseCommand_Map(t *testing.T) {
	result := parseCommand(map[string]interface{}{
		"task1": "echo first",
		"task2": "echo second",
	})
	// Maps have no guaranteed order, so just check length
	assert.Len(t, result, 2)
	// All should be shell commands since they're strings
	for _, cmd := range result {
		assert.True(t, cmd.UseShell, "string commands in map should use shell")
		assert.NotEmpty(t, cmd.Name, "named commands should have names")
	}
}

func TestParseCommand_MapWithArrayCommand(t *testing.T) {
	result := parseCommand(map[string]interface{}{
		"task1": []interface{}{"npm", "install", "--save"},
	})
	require.Len(t, result, 1)
	assert.Equal(t, []string{"npm", "install", "--save"}, result[0].Args)
	assert.False(t, result[0].UseShell, "array commands in map should not use shell")
	assert.Equal(t, "task1", result[0].Name)
}

func TestParseCommand_Nil(t *testing.T) {
	result := parseCommand(nil)
	assert.Nil(t, result)
}

func TestParseCommand_EmptyArray(t *testing.T) {
	result := parseCommand([]string{})
	assert.Nil(t, result)
}

func TestParseCommand_ArrayWithSpaces(t *testing.T) {
	// This is the key test case - array commands with spaces in arguments
	// should preserve the arguments separately, not join them
	result := parseCommand([]interface{}{"git", "clone", "https://example.com/repo with spaces.git"})
	require.Len(t, result, 1)
	assert.Equal(t, []string{"git", "clone", "https://example.com/repo with spaces.git"}, result[0].Args)
	assert.False(t, result[0].UseShell, "array commands should use exec semantics")
}

func TestFormatCommandForDisplay(t *testing.T) {
	tests := []struct {
		name     string
		cmd      CommandSpec
		expected string
	}{
		{
			name:     "simple command",
			cmd:      CommandSpec{Args: []string{"echo", "hello"}},
			expected: "echo hello",
		},
		{
			name:     "named command",
			cmd:      CommandSpec{Args: []string{"npm", "install"}, Name: "install"},
			expected: "[install] npm install",
		},
		{
			name:     "shell command",
			cmd:      CommandSpec{Args: []string{"echo hello && echo world"}, UseShell: true},
			expected: "echo hello && echo world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatCommandForDisplay(tt.cmd)
			assert.Equal(t, tt.expected, result)
		})
	}
}

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
			runner := &HookRunner{
				cfg: &devcontainer.DevContainerConfig{
					WaitFor: tt.waitFor,
				},
			}
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
			runner := &HookRunner{
				cfg: &devcontainer.DevContainerConfig{
					WaitFor: tt.waitFor,
				},
			}
			result := runner.getWaitFor()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHookRunnerWithRemoteEnv(t *testing.T) {
	// Verify HookRunner stores remoteEnv in config for use during command execution
	// The actual application of remoteEnv in executeContainerCommand is tested via e2e tests
	cfg := &devcontainer.DevContainerConfig{
		RemoteEnv: map[string]string{
			"EDITOR": "vim",
			"TERM":   "xterm-256color",
		},
		RemoteUser: "vscode",
	}

	runner := &HookRunner{
		cfg: cfg,
	}

	// Verify config is accessible
	require.NotNil(t, runner.cfg)
	assert.Equal(t, "vim", runner.cfg.RemoteEnv["EDITOR"])
	assert.Equal(t, "xterm-256color", runner.cfg.RemoteEnv["TERM"])
	assert.Equal(t, "vscode", runner.cfg.RemoteUser)
}

func TestHookRunnerRemoteEnvNil(t *testing.T) {
	// Verify HookRunner handles nil remoteEnv gracefully
	cfg := &devcontainer.DevContainerConfig{
		RemoteEnv: nil,
	}

	runner := &HookRunner{
		cfg: cfg,
	}

	require.NotNil(t, runner.cfg)
	assert.Nil(t, runner.cfg.RemoteEnv)
}
