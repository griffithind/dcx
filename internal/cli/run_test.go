package cli

import (
	"bytes"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const shortcutsConfig = `{
	"image": "alpine:3",
	"customizations": {
		"dcx": {
			"shortcuts": {
				"hello": "echo hello from shortcut",
				"greet": {"command": "echo greeting", "description": "Say hello"},
				"say": {"prefix": "echo", "passArgs": true, "description": "Echo with args"}
			}
		}
	}
}`

func TestRunListShowsShortcuts(t *testing.T) {
	withFakeEngine(t, &containertest.Fake{}, shortcutsConfig)
	runList = true
	t.Cleanup(func() { runList = false })
	out := captureUI(t, func() { require.NoError(t, runRunCommand(runCmd, nil)) })
	assert.Contains(t, out, "Available shortcuts:")
	for _, want := range []string{"hello", "echo hello from shortcut", "greet", "Say hello", "say", "Echo with args"} {
		assert.Contains(t, out, want)
	}
}

func TestRunWithoutShortcuts(t *testing.T) {
	withFakeEngine(t, &containertest.Fake{}, `{"image": "alpine:3"}`)

	runList = true
	t.Cleanup(func() { runList = false })
	out := captureUI(t, func() { require.NoError(t, runRunCommand(runCmd, nil)) })
	assert.Contains(t, out, "No shortcuts defined")

	runList = false
	err := runRunCommand(runCmd, []string{"anything"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no shortcuts defined")
}

func TestRunRejectsUnknownOrMissingShortcut(t *testing.T) {
	withFakeEngine(t, &containertest.Fake{}, shortcutsConfig)

	err := runRunCommand(runCmd, []string{"nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown shortcut "nope"`)

	err = runRunCommand(runCmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no shortcut specified")
}

func TestCompletionScripts(t *testing.T) {
	for shell, want := range map[string]string{"bash": "bash completion", "zsh": "zsh completion", "fish": "fish completion"} {
		t.Run(shell, func(t *testing.T) {
			var buf bytes.Buffer
			rootCmd.SetOut(&buf)
			rootCmd.SetArgs([]string{"completion", shell})
			t.Cleanup(func() {
				rootCmd.SetOut(ui.NewCobraOutWriter())
				rootCmd.SetArgs(nil)
			})
			require.NoError(t, rootCmd.Execute())
			assert.Contains(t, buf.String(), want)
		})
	}
}

func TestRunCompletesShortcutNames(t *testing.T) {
	withFakeEngine(t, &containertest.Fake{}, shortcutsConfig)
	names, directive := completeShortcuts(runCmd, nil, "")
	assert.ElementsMatch(t, []string{"greet\tSay hello", "hello", "say\tEcho with args"}, names)
	assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)

	_, directive = completeShortcuts(runCmd, []string{"say"}, "")
	assert.Equal(t, cobra.ShellCompDirectiveDefault, directive, "arguments after the shortcut complete as files")
}
