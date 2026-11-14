// Package cli implements the command-line interface for dcx.
package cli

import (
	"fmt"
	"os"
	"path/filepath"

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

		path, err := resolveWorkspacePath(workspacePath)
		if err != nil {
			return err
		}
		workspacePath = path
		// A relative --config is relative to the current directory, like
		// every other path argument (and what shell completion produces),
		// not to the workspace.
		if configPath != "" && !filepath.IsAbs(configPath) {
			abs, err := filepath.Abs(configPath)
			if err != nil {
				return fmt.Errorf("invalid config path %q: %w", configPath, err)
			}
			configPath = abs
		}
		return nil
	},
}

// resolveWorkspacePath returns the absolute workspace path for the -w flag
// (the current directory when empty). Everything derived from it (the
// workspace ID, ${localWorkspaceFolder}, labels, bind sources) needs an
// absolute path; a relative one leaked into all of them.
func resolveWorkspacePath(path string) (string, error) {
	if path == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current directory: %w", err)
		}
		return wd, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid workspace path %q: %w", path, err)
	}
	return abs, nil
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() error {
	// Errors cobra reports before PersistentPreRunE (a bad flag, an unknown
	// command) must honour --no-color and the terminal check too.
	preconfigureUI(os.Args[1:])
	cmd, err := rootCmd.ExecuteC()
	if err != nil {
		if jsonRequested(cmd) {
			// A consumer of --json output gets the failure as JSON too.
			_ = printJSON(map[string]string{"error": err.Error()})
		} else {
			ui.PrintError(err)
		}
	}
	return err
}

// jsonRequested reports whether cmd was run with --json.
func jsonRequested(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	f := cmd.Flags().Lookup("json")
	return f != nil && f.Value.String() == "true"
}

// preconfigureUI applies --no-color from the raw arguments (up to "--"),
// before cobra has parsed them.
func preconfigureUI(args []string) {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--no-color" || arg == "--no-color=true" {
			noColor = true
		}
	}
	initUI()
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
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "path to devcontainer.json, relative to the current directory (default: auto-detect in the workspace)")

	// Output flags
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "print only data and errors (no progress or warnings)")
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
