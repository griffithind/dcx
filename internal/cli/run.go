package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/shortcuts"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var runList bool

var runCmd = &cobra.Command{
	Use:   "run [shortcut] [args...]",
	Short: "Run a command shortcut in the container",
	Long: `Run a configured command shortcut in the devcontainer.

Shortcuts are defined in devcontainer.json under "customizations.dcx.shortcuts".

Example devcontainer.json:
{
  "name": "myproject",
  "image": "ubuntu",
  "customizations": {
    "dcx": {
      "shortcuts": {
        "rw": "bin/jobs --skip-recurring",
        "r": {"prefix": "rails", "passArgs": true},
        "test": {"prefix": "rails test", "passArgs": true, "description": "Run tests"}
      }
    }
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
	runCmd.Flags().BoolVarP(&runList, "list", "l", false, "list available shortcuts")
	// Stop parsing flags after the shortcut name so args like --version pass through
	runCmd.Flags().SetInterspersed(false)
	runCmd.GroupID = "execution"
	rootCmd.AddCommand(runCmd)
}

func runRunCommand(cmd *cobra.Command, args []string) error {
	// Load devcontainer.json for shortcuts
	cfg, _, err := devcontainer.Load(workspacePath, configPath)
	if err != nil {
		return fmt.Errorf("failed to load devcontainer.json: %w", err)
	}

	// Get DCX customizations
	dcxCustom := devcontainer.GetDcxCustomizations(cfg)

	// Handle --list flag
	if runList {
		return listShortcuts(dcxCustom)
	}

	if len(args) == 0 {
		return fmt.Errorf("no shortcut specified; use --list to see available shortcuts")
	}

	// Resolve shortcut
	if dcxCustom == nil || len(dcxCustom.Shortcuts) == 0 {
		return fmt.Errorf("no shortcuts defined in devcontainer.json customizations.dcx")
	}

	resolved := shortcuts.Resolve(dcxCustom.Shortcuts, args)
	if !resolved.Found {
		return fmt.Errorf("unknown shortcut %q; use --list to see available shortcuts", args[0])
	}

	// Execute the resolved command
	return executeInContainer(resolved.Command)
}

func listShortcuts(dcxCustom *devcontainer.DcxCustomizations) error {
	if dcxCustom == nil || len(dcxCustom.Shortcuts) == 0 {
		ui.Println("No shortcuts defined.")
		ui.Println("")
		ui.Println("To define shortcuts, add \"customizations.dcx.shortcuts\" to devcontainer.json.")
		return nil
	}

	infos := shortcuts.ListShortcuts(dcxCustom.Shortcuts)

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
	cfg, _, _ := devcontainer.Load(cliCtx.WorkspacePath(), cliCtx.ConfigPath())

	// Execute command using ExecBuilder
	builder := NewExecBuilder(containerInfo, cfg, cliCtx.WorkspacePath())
	return builder.Execute(cliCtx.Ctx, ExecFlags{
		Command: execArgs,
	})
}
