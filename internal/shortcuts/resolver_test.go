package shortcuts

import (
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
)

func TestResolve(t *testing.T) {
	shortcuts := map[string]devcontainer.Shortcut{
		"rw": {Command: "bin/jobs --skip-recurring"},
		"r":  {Prefix: "rails", PassArgs: true},
		"rs": {Command: "rails server -b 0.0.0.0"},
		"be": {Prefix: "bundle exec"},          // No passArgs
		"test": {Prefix: "rails test", PassArgs: true},
	}

	tests := []struct {
		name     string
		args     []string
		expected ResolvedCommand
	}{
		{
			name: "simple command shortcut",
			args: []string{"rw"},
			expected: ResolvedCommand{
				Command: []string{"bin/jobs", "--skip-recurring"},
				Found:   true,
			},
		},
		{
			name: "command shortcut with extra args",
			args: []string{"rw", "--verbose"},
			expected: ResolvedCommand{
				Command: []string{"bin/jobs", "--skip-recurring", "--verbose"},
				Found:   true,
			},
		},
		{
			name: "prefix with passArgs - single arg",
			args: []string{"r", "console"},
			expected: ResolvedCommand{
				Command: []string{"rails", "console"},
				Found:   true,
			},
		},
		{
			name: "prefix with passArgs - multiple args",
			args: []string{"r", "server", "-p", "3001"},
			expected: ResolvedCommand{
				Command: []string{"rails", "server", "-p", "3001"},
				Found:   true,
			},
		},
		{
			name: "prefix with passArgs - no extra args",
			args: []string{"r"},
			expected: ResolvedCommand{
				Command: []string{"rails"},
				Found:   true,
			},
		},
		{
			name: "prefix without passArgs",
			args: []string{"be"},
			expected: ResolvedCommand{
				Command: []string{"bundle", "exec"},
				Found:   true,
			},
		},
		{
			name: "prefix without passArgs ignores extra args",
			args: []string{"be", "rspec"},
			expected: ResolvedCommand{
				Command: []string{"bundle", "exec"},
				Found:   true,
			},
		},
		{
			name: "multi-word prefix with passArgs",
			args: []string{"test", "test/models/"},
			expected: ResolvedCommand{
				Command: []string{"rails", "test", "test/models/"},
				Found:   true,
			},
		},
		{
			name: "unknown shortcut",
			args: []string{"unknown", "arg"},
			expected: ResolvedCommand{
				Command: []string{"unknown", "arg"},
				Found:   false,
			},
		},
		{
			name: "empty args",
			args: []string{},
			expected: ResolvedCommand{
				Command: []string{},
				Found:   false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Resolve(shortcuts, tt.args)
			assert.Equal(t, tt.expected.Found, result.Found)
			assert.Equal(t, tt.expected.Command, result.Command)
		})
	}
}

func TestResolveEmptyShortcuts(t *testing.T) {
	result := Resolve(nil, []string{"rw"})
	assert.False(t, result.Found)
	assert.Equal(t, []string{"rw"}, result.Command)

	result = Resolve(map[string]devcontainer.Shortcut{}, []string{"rw"})
	assert.False(t, result.Found)
	assert.Equal(t, []string{"rw"}, result.Command)
}

func TestListShortcuts(t *testing.T) {
	shortcuts := map[string]devcontainer.Shortcut{
		"rw":   {Command: "bin/jobs --skip-recurring", Description: "Run workers"},
		"r":    {Prefix: "rails", PassArgs: true, Description: "Rails command"},
		"rs":   {Command: "rails server -b 0.0.0.0"},
		"test": {Prefix: "rails test", PassArgs: true},
	}

	infos := ListShortcuts(shortcuts)

	// Should be sorted alphabetically
	assert.Len(t, infos, 4)
	assert.Equal(t, "r", infos[0].Name)
	assert.Equal(t, "rs", infos[1].Name)
	assert.Equal(t, "rw", infos[2].Name)
	assert.Equal(t, "test", infos[3].Name)

	// Check expansion
	assert.Equal(t, "rails [args...]", infos[0].Expansion)
	assert.Equal(t, "rails server -b 0.0.0.0", infos[1].Expansion)
	assert.Equal(t, "bin/jobs --skip-recurring", infos[2].Expansion)
	assert.Equal(t, "rails test [args...]", infos[3].Expansion)

	// Check descriptions
	assert.Equal(t, "Rails command", infos[0].Description)
	assert.Empty(t, infos[1].Description)
	assert.Equal(t, "Run workers", infos[2].Description)
	assert.Empty(t, infos[3].Description)
}

func TestListShortcutsEmpty(t *testing.T) {
	result := ListShortcuts(nil)
	assert.Nil(t, result)

	result = ListShortcuts(map[string]devcontainer.Shortcut{})
	assert.Nil(t, result)
}

func TestListShortcutsSorting(t *testing.T) {
	// Test that shortcuts are sorted alphabetically regardless of insertion order
	shortcuts := map[string]devcontainer.Shortcut{
		"z": {Command: "cmd-z"},
		"a": {Command: "cmd-a"},
		"m": {Command: "cmd-m"},
	}

	infos := ListShortcuts(shortcuts)
	assert.Len(t, infos, 3)
	assert.Equal(t, "a", infos[0].Name)
	assert.Equal(t, "m", infos[1].Name)
	assert.Equal(t, "z", infos[2].Name)
}

func TestResolveWithFlags(t *testing.T) {
	// Test that flags are passed through correctly
	shortcuts := map[string]devcontainer.Shortcut{
		"r": {Prefix: "rails", PassArgs: true},
	}

	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "single dash flag",
			args:     []string{"r", "-v"},
			expected: []string{"rails", "-v"},
		},
		{
			name:     "double dash flag",
			args:     []string{"r", "--version"},
			expected: []string{"rails", "--version"},
		},
		{
			name:     "flag with value",
			args:     []string{"r", "-p", "3001"},
			expected: []string{"rails", "-p", "3001"},
		},
		{
			name:     "equals style flag",
			args:     []string{"r", "--port=3001"},
			expected: []string{"rails", "--port=3001"},
		},
		{
			name:     "mixed args and flags",
			args:     []string{"r", "server", "-b", "0.0.0.0", "--port", "3001"},
			expected: []string{"rails", "server", "-b", "0.0.0.0", "--port", "3001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Resolve(shortcuts, tt.args)
			assert.True(t, result.Found)
			assert.Equal(t, tt.expected, result.Command)
		})
	}
}

func TestResolveCommandWithFlags(t *testing.T) {
	// Test simple command shortcuts with additional args
	shortcuts := map[string]devcontainer.Shortcut{
		"rw": {Command: "bin/jobs --skip-recurring"},
	}

	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "command with no extra args",
			args:     []string{"rw"},
			expected: []string{"bin/jobs", "--skip-recurring"},
		},
		{
			name:     "command with extra args appended",
			args:     []string{"rw", "--verbose"},
			expected: []string{"bin/jobs", "--skip-recurring", "--verbose"},
		},
		{
			name:     "command with multiple extra args",
			args:     []string{"rw", "--verbose", "--count", "5"},
			expected: []string{"bin/jobs", "--skip-recurring", "--verbose", "--count", "5"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Resolve(shortcuts, tt.args)
			assert.True(t, result.Found)
			assert.Equal(t, tt.expected, result.Command)
		})
	}
}

func TestResolveSpecialCharacters(t *testing.T) {
	shortcuts := map[string]devcontainer.Shortcut{
		"echo": {Prefix: "echo", PassArgs: true},
	}

	tests := []struct {
		name     string
		args     []string
		expected []string
	}{
		{
			name:     "args with spaces in quotes",
			args:     []string{"echo", "hello world"},
			expected: []string{"echo", "hello world"},
		},
		{
			name:     "args with special chars",
			args:     []string{"echo", "$HOME", "~", "*"},
			expected: []string{"echo", "$HOME", "~", "*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Resolve(shortcuts, tt.args)
			assert.True(t, result.Found)
			assert.Equal(t, tt.expected, result.Command)
		})
	}
}
