// Package cli implements the command-line interface for dcx.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/version"
)

// Global flags
var (
	workspacePath string
	configPath    string
	jsonOutput    bool
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

It uses the Docker Engine API and docker compose CLI directly, without
requiring the @devcontainers/cli. Container state is tracked using labels,
enabling offline-safe operations for start/stop/exec commands.`,
	Version: version.Version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Configure output system
		format := output.FormatText
		if jsonOutput {
			format = output.FormatJSON
		}

		verbosity := output.VerbosityNormal
		if quiet {
			verbosity = output.VerbosityQuiet
		} else if verbose {
			verbosity = output.VerbosityVerbose
		}

		output.Configure(output.Config{
			Format:    format,
			Verbosity: verbosity,
			NoColor:   noColor,
			Writer:    os.Stdout,
			ErrWriter: os.Stderr,
		})

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
	return rootCmd.Execute()
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&workspacePath, "workspace", "w", "", "workspace directory (default: current directory)")
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "", "path to devcontainer.json (default: auto-detect)")

	// Output flags
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "disable colored output")
	rootCmd.PersistentFlags().BoolVarP(&quiet, "quiet", "q", false, "minimal output (errors only)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	// Add subcommands
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(downCmd)
	rootCmd.AddCommand(doctorCmd)
}
