package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/shortcuts"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	runNoAgent bool
	runList    bool
)

var runCmd = &cobra.Command{
	Use:   "run [shortcut] [args...]",
	Short: "Run a command shortcut in the container",
	Long: `Run a configured command shortcut in the devcontainer.

Shortcuts are defined in .devcontainer/dcx.json under the "shortcuts" key.

Example dcx.json:
{
  "name": "myproject",
  "shortcuts": {
    "rw": "bin/jobs --skip-recurring",
    "r": {"prefix": "rails", "passArgs": true},
    "test": {"prefix": "rails test", "passArgs": true, "description": "Run tests"}
  }
}

Usage:
  dcx run rw                    # Runs: bin/jobs --skip-recurring
  dcx run r server              # Runs: rails server
  dcx run r console             # Runs: rails console
  dcx run test test/models/     # Runs: rails test test/models/

Use --list to see all available shortcuts.`,
	RunE: runRunCommand,
	Args: cobra.ArbitraryArgs,
}

func init() {
	runCmd.Flags().BoolVar(&runNoAgent, "no-agent", false, "disable SSH agent forwarding")
	runCmd.Flags().BoolVarP(&runList, "list", "l", false, "list available shortcuts")
	// Stop parsing flags after the shortcut name so args like --version pass through
	runCmd.Flags().SetInterspersed(false)
	runCmd.GroupID = "execution"
	rootCmd.AddCommand(runCmd)
}

func runRunCommand(cmd *cobra.Command, args []string) error {
	// Load dcx.json for shortcuts
	dcxCfg, err := config.LoadDcxConfig(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to load dcx.json: %w", err)
	}

	// Handle --list flag
	if runList {
		return listShortcuts(dcxCfg)
	}

	if len(args) == 0 {
		return fmt.Errorf("no shortcut specified; use --list to see available shortcuts")
	}

	// Resolve shortcut
	if dcxCfg == nil || len(dcxCfg.Shortcuts) == 0 {
		return fmt.Errorf("no shortcuts defined in .devcontainer/dcx.json")
	}

	resolved := shortcuts.Resolve(dcxCfg.Shortcuts, args)
	if !resolved.Found {
		return fmt.Errorf("unknown shortcut %q; use --list to see available shortcuts", args[0])
	}

	// Execute the resolved command
	return executeInContainer(resolved.Command)
}

func listShortcuts(dcxCfg *config.DcxConfig) error {
	if dcxCfg == nil || len(dcxCfg.Shortcuts) == 0 {
		ui.Println("No shortcuts defined.")
		ui.Println("")
		ui.Println("To define shortcuts, create .devcontainer/dcx.json with a \"shortcuts\" key.")
		return nil
	}

	infos := shortcuts.ListShortcuts(dcxCfg.Shortcuts)

	ui.Println(ui.Bold("Available shortcuts:"))
	ui.Println("")

	headers := []string{"Shortcut", "Command", "Description"}
	rows := make([][]string, 0, len(infos))
	for _, info := range infos {
		rows = append(rows, []string{info.Name, info.Expansion, info.Description})
	}

	return ui.RenderTable(headers, rows)
}

func executeInContainer(execArgs []string) error {
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
	cfg, _, _ := config.Load(cliCtx.WorkspacePath(), cliCtx.ConfigPath())

	// Execute command using ExecBuilder
	builder := NewExecBuilder(containerInfo, cfg, cliCtx.WorkspacePath())
	return builder.Execute(cliCtx.Ctx, ExecFlags{
		Command:        execArgs,
		EnableSSHAgent: !runNoAgent,
	})
}
