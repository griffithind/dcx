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

Use --list to see all available shortcuts.`,
	Example: `  dcx run rw                    # Runs: bin/jobs --skip-recurring
  dcx run r server              # Runs: rails server
  dcx run r console             # Runs: rails console
  dcx run test test/models/     # Runs: rails test test/models/`,
	RunE:              runRunCommand,
	Args:              cobra.ArbitraryArgs,
	ValidArgsFunction: completeShortcuts,
}

// completeShortcuts completes the shortcut name (the first argument) from
// devcontainer.json; later arguments complete as files.
func completeShortcuts(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	cfg, _, err := devcontainer.Load(workspacePath, configPath)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	custom := cfg.DcxCustomizations()
	if custom == nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, info := range shortcuts.ListShortcuts(custom.Shortcuts) {
		entry := info.Name
		if info.Description != "" {
			entry += "\t" + info.Description
		}
		names = append(names, entry)
	}
	return names, cobra.ShellCompDirectiveNoFileComp
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
	dcxCustom := cfg.DcxCustomizations()

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

	return sshInto(resolved.CommandLine)
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
