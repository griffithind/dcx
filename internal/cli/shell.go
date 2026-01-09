package cli

import (
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/spf13/cobra"
)

var shellNoAgent bool

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open an interactive shell",
	Long: `Open an interactive shell in the running devcontainer.

By default, SSH agent forwarding is enabled if available. Use --no-agent
to disable it.

The shell used is /bin/bash if available, otherwise /bin/sh.`,
	RunE: runShell,
}

func init() {
	shellCmd.Flags().BoolVar(&shellNoAgent, "no-agent", false, "disable SSH agent forwarding")
	shellCmd.GroupID = "execution"
	rootCmd.AddCommand(shellCmd)
}

func runShell(cmd *cobra.Command, args []string) error {
	// Initialize CLI context
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	// Validate container is running
	containerInfo, err := RequireRunningContainer(cliCtx)
	if err != nil {
		return err
	}

	// Load config
	cfg, _, _ := devcontainer.Load(cliCtx.WorkspacePath(), cliCtx.ConfigPath())

	// Open shell using ExecBuilder
	builder := NewExecBuilder(containerInfo, cfg, cliCtx.WorkspacePath())
	return builder.Shell(cliCtx.Ctx, "/bin/bash", !shellNoAgent)
}
