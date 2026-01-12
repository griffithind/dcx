package cli

import (
	"fmt"
	"os"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/ssh/client"
	"github.com/spf13/cobra"
)

var execCmd = &cobra.Command{
	Use:   "exec -- <command> [args...]",
	Short: "Run a command in the container",
	Long: `Run a command inside the running devcontainer.

SSH agent forwarding is automatically enabled when available.

Examples:
  dcx exec -- npm install
  dcx exec -- ls -la /workspace
  dcx exec -- git clone git@github.com:user/repo.git
  dcx exec -- bash -c "echo hello"`,
	RunE: runExec,
	// Args after "--" are passed directly to the command
	Args: cobra.ArbitraryArgs,
}

func runExec(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no command specified; usage: dcx exec -- <command> [args...]")
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

	// Execute via unified SSH path
	exitCode, err := client.ExecInContainer(cliCtx.Ctx, client.ContainerExecOptions{
		ContainerName: containerInfo.Name,
		Config:        cfg,
		WorkspacePath: cliCtx.WorkspacePath(),
		Command:       args,
	})

	if err != nil {
		return fmt.Errorf("exec failed: %w", err)
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}

func init() {
	execCmd.GroupID = "execution"
	rootCmd.AddCommand(execCmd)
}
