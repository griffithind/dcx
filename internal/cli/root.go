// Package cli implements the command-line interface for dcx.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/griffithind/dcx/internal/ui"
	"github.com/griffithind/dcx/internal/version"
)

// Global flags
var (
	workspacePath string
	configPath    string
	noColor       bool
	quiet         bool
	verbose       bool
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "dcx",
	Short: "Devcontainer Executor",
	Long: `dcx is a CLI that parses, validates, and runs devcontainers
with full support for docker compose and Features.

It drives the docker and docker compose CLIs directly, without the
@devcontainers/cli, and tracks all state in container labels. Sessions go
over SSH to a small agent inside the container: use 'dcx ssh' or plain
'ssh <alias>'.`,
	Version: version.Version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Cobra has parsed the flags by now, so --quiet/--verbose/--no-color
		// apply to everything the command prints.
		initUI()

		// Flags parsed successfully, so any error from here on is a runtime
		// failure: report it without dumping the command's usage text.
		// (Flag and argument errors happen earlier and still show usage.)
		cmd.SilenceUsage = true

		// Initialize workspace path if not provided
		if workspacePath == "" {
			var err error
			workspacePath, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("failed to get current directory: %w", err)
			}
		}
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	err := rootCmd.Execute()
	if err != nil {
		ui.PrintError(err)
	}
	return err
}

// initUI configures the UI system based on parsed flags.
func initUI() {
	verbosity := ui.VerbosityNormal
	if quiet {
		verbosity = ui.VerbosityQuiet
	} else if verbose {
		verbosity = ui.VerbosityVerbose
	}

	ui.Configure(ui.Config{
		Verbosity: verbosity,
		NoColor:   noColor,
		Writer:    os.Stdout,
		ErrWriter: os.Stderr,
	})
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&workspacePath, "workspace", "w", "", "workspace directory (default: current directory)")
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "path to devcontainer.json (default: auto-detect)")

	// Output flags
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "minimal output (errors only)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	// Configure Cobra to use UI-aware writers
	rootCmd.SetOut(ui.NewCobraOutWriter())
	rootCmd.SetErr(ui.NewCobraErrWriter())
	rootCmd.SilenceErrors = true // We handle errors ourselves in Execute()

	// Define command groups
	rootCmd.AddGroup(&cobra.Group{ID: "lifecycle", Title: "Lifecycle Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "execution", Title: "Execution Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "info", Title: "Information Commands:"})
	rootCmd.AddGroup(&cobra.Group{ID: "maintenance", Title: "Build & Maintenance:"})
	rootCmd.AddGroup(&cobra.Group{ID: "utilities", Title: "Utilities:"})

	// Lifecycle commands
	upCmd.GroupID = "lifecycle"
	stopCmd.GroupID = "lifecycle"
	downCmd.GroupID = "lifecycle"
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(downCmd)

	// Information commands
	statusCmd.GroupID = "info"
	rootCmd.AddCommand(statusCmd)

	// Utilities
	doctorCmd.GroupID = "utilities"
	rootCmd.AddCommand(doctorCmd)
}
