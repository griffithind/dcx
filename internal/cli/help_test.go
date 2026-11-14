package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// TestHelpGolden pins every command's --help: the commands, flags, groups
// and descriptions users see. Review the diff of
// go test ./internal/cli -run TestHelpGolden -args -update
func TestHelpGolden(t *testing.T) {
	var out strings.Builder
	var walk func(cmd *cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Hidden || (cmd.Name() == "help" && cmd != rootCmd) {
			return
		}
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		require.NoError(t, cmd.Help())
		if cmd == rootCmd {
			cmd.SetOut(ui.NewCobraOutWriter())
		} else {
			cmd.SetOut(nil) // inherit the root's
		}
		out.WriteString("=== " + cmd.CommandPath() + "\n")
		out.WriteString(buf.String())
		out.WriteString("\n")
		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
	golden(t, "help", out.String())
}
