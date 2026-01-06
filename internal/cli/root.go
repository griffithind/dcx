// Package cli implements the command-line interface for dcx.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via ldflags.
var Version = "dev"

// Global flags
var (
	workspacePath string
	configPath    string
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
	Version: Version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
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
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	// Add subcommands
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(upCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(downCmd)
	rootCmd.AddCommand(doctorCmd)
}
