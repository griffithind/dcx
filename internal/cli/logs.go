package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	logsFollow     bool
	logsTail       string
	logsTimestamps bool
)

var logsCmd = &cobra.Command{
	Use:   "logs [flags]",
	Short: "View container logs",
	Long: `View logs from the devcontainer's primary container.

By default, shows the last 100 lines of logs. Use --follow to stream
new log output in real-time.

Examples:
  dcx logs                # Show last 100 lines
  dcx logs --follow       # Stream logs in real-time
  dcx logs --tail 50      # Show last 50 lines
  dcx logs --timestamps   # Include timestamps`,
	RunE: runLogs,
}

func runLogs(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Load dcx.json for project name
	dcxCfg, _ := config.LoadDcxConfig(workspacePath)

	// Get project name from dcx.json
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = docker.SanitizeProjectName(dcxCfg.Name)
	}

	// Initialize state manager
	stateMgr := state.NewManager(dockerClient)
	envKey := workspace.ComputeID(workspacePath)

	// Get current state
	currentState, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if currentState == state.StateAbsent {
		return fmt.Errorf("no environment found; run 'dcx up' first")
	}

	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Get logs from container
	opts := docker.LogsOptions{
		Follow:     logsFollow,
		Timestamps: logsTimestamps,
		Tail:       logsTail,
	}

	reader, err := dockerClient.GetLogs(ctx, containerInfo.ID, opts)
	if err != nil {
		return fmt.Errorf("failed to get logs: %w", err)
	}
	defer reader.Close()

	// Stream logs to stdout
	_, err = io.Copy(os.Stdout, reader)
	if err != nil && err != io.EOF {
		return fmt.Errorf("failed to read logs: %w", err)
	}

	return nil
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "follow log output")
	logsCmd.Flags().StringVar(&logsTail, "tail", "100", "number of lines to show from the end (use 'all' for all logs)")
	logsCmd.Flags().BoolVarP(&logsTimestamps, "timestamps", "t", false, "show timestamps")
	logsCmd.GroupID = "info"
	rootCmd.AddCommand(logsCmd)
}

// ValidateTail validates the tail parameter.
func validateTail(tail string) bool {
	if tail == "all" {
		return true
	}
	_, err := strconv.Atoi(tail)
	return err == nil
}
