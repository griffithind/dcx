package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/spf13/cobra"
)

var execNoAgent bool

var execCmd = &cobra.Command{
	Use:   "exec [--no-agent] -- <command> [args...]",
	Short: "Run a command in the container",
	Long: `Run a command inside the running devcontainer.

By default, SSH agent forwarding is enabled if available. Use --no-agent
to disable it.

Examples:
  dcx exec -- npm install
  dcx exec -- ls -la /workspace
  dcx exec -- git clone git@github.com:user/repo.git
  dcx exec --no-agent -- bash -c "echo hello"`,
	RunE: runExec,
	// Args after "--" are passed directly to the command
	Args: cobra.ArbitraryArgs,
}

func runExec(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command specified; usage: dcx exec [--no-agent] -- <command> [args...]")
	}

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

	// Execute command using ExecBuilder
	builder := NewExecBuilder(containerInfo, cfg, cliCtx.WorkspacePath())
	return builder.Execute(cliCtx.Ctx, ExecFlags{
		Command:        args,
		EnableSSHAgent: !execNoAgent,
	})
}

func init() {
	execCmd.Flags().BoolVar(&execNoAgent, "no-agent", false, "disable SSH agent forwarding")
	execCmd.GroupID = "execution"
	rootCmd.AddCommand(execCmd)
}
