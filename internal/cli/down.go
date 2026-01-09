package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/service"
	"github.com/spf13/cobra"
)

var (
	removeVolumes bool
	removeOrphans bool
)

var downCmd = &cobra.Command{
	Use:   "down",
	Short: "Stop and remove containers",
	Long: `Stop and remove devcontainer containers.

This is an offline-safe command that stops and removes containers
managed by dcx. Optionally removes volumes and orphan containers.`,
	RunE: runDown,
}

func init() {
	downCmd.Flags().BoolVar(&removeVolumes, "volumes", false, "remove named volumes")
	downCmd.Flags().BoolVar(&removeOrphans, "remove-orphans", false, "remove containers not defined in compose file")
}

func runDown(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := container.NewDockerClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Create service and get identifiers
	svc := service.NewDevContainerService(dockerClient, workspacePath, configPath, verbose)
	defer svc.Close()

	ids, err := svc.GetIdentifiers()
	if err != nil {
		return fmt.Errorf("failed to get identifiers: %w", err)
	}

	return svc.DownWithIDs(ctx, ids.ProjectName, ids.WorkspaceID, service.DownOptions{
		RemoveVolumes: removeVolumes,
		RemoveOrphans: removeOrphans,
	})
}
