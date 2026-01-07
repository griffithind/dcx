package lifecycle

import (
	"testing"

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
