package cli

import (
	"os"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/ssh/client"
	"github.com/spf13/cobra"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Open an interactive shell",
	Long: `Open an interactive shell in the running devcontainer.

SSH agent forwarding is automatically enabled when available.

The shell used is the container's default login shell.`,
	RunE: runShell,
}

func init() {
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

	// Open interactive shell via unified SSH path
	tty := true
	exitCode, err := client.ExecInContainer(cliCtx.Ctx, client.ContainerExecOptions{
		ContainerName: containerInfo.Name,
		Config:        cfg,
		WorkspacePath: cliCtx.WorkspacePath(),
		Command:       nil, // nil = interactive shell
		TTY:           &tty,
	})
	if err != nil {
		return err
	}
	if exitCode != 0 {
		os.Exit(exitCode)
	}
	return nil
}
