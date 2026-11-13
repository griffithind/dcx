package shortcuts

import (
	"os/exec"
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	shortcuts := map[string]devcontainer.Shortcut{
		"rw":   {Command: "bin/jobs --skip-recurring"},
		"r":    {Prefix: "rails", PassArgs: true},
		"be":   {Prefix: "bundle exec"}, // No passArgs
		"pipe": {Command: "cd /src && ls | wc -l"},
	}

	tests := []struct {
		name string
		args []string
		want ResolvedCommand
	}{
		{"command", []string{"rw"}, ResolvedCommand{`sh -c 'bin/jobs --skip-recurring'`, true}},
		{"command gets args", []string{"rw", "--verbose"}, ResolvedCommand{`sh -c 'bin/jobs --skip-recurring "$@"' dcx-run --verbose`, true}},
		{"prefix with passArgs", []string{"r", "server", "-p", "3001"}, ResolvedCommand{`sh -c 'rails "$@"' dcx-run server -p 3001`, true}},
		{"prefix without passArgs drops args", []string{"be", "rake"}, ResolvedCommand{`sh -c 'bundle exec'`, true}},
		{"shell syntax is kept", []string{"pipe"}, ResolvedCommand{`sh -c 'cd /src && ls | wc -l'`, true}},
		{"unknown", []string{"unknown", "arg"}, ResolvedCommand{}},
		{"empty", nil, ResolvedCommand{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, Resolve(shortcuts, tt.args))
		})
	}
}

// The command line is run by the remote user's shell; arguments must reach
// the shortcut exactly, whatever they contain.
func TestResolvePreservesArguments(t *testing.T) {
	shortcuts := map[string]devcontainer.Shortcut{
		"args": {Prefix: `printf '[%s]'`, PassArgs: true},
	}
	args := []string{"a b", "it's", `$HOME`, "*", "--flag=x;y"}
	resolved := Resolve(shortcuts, append([]string{"args"}, args...))
	require.True(t, resolved.Found)

	out, err := exec.Command("sh", "-c", resolved.CommandLine).Output()
	require.NoError(t, err)
	assert.Equal(t, "[a b][it's][$HOME][*][--flag=x;y]", string(out))
}

func TestResolveEmptyShortcuts(t *testing.T) {
	assert.False(t, Resolve(nil, []string{"x"}).Found)
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
