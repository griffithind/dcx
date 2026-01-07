package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/service"
	"github.com/spf13/cobra"
)

var (
	noCache   bool
	pullBuild bool
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build the devcontainer images",
	Long: `Build the devcontainer images without starting containers.

This command parses the devcontainer.json configuration and builds
any required images. For compose-based configurations, it runs
'docker compose build'. For image-based configurations, it pulls
the image. For Dockerfile-based configurations, it builds the image.

This command may require network access for pulling base images.`,
	RunE: runBuild,
}

func init() {
	buildCmd.Flags().BoolVar(&noCache, "no-cache", false, "build without using cache")
	buildCmd.Flags().BoolVar(&pullBuild, "pull", false, "force re-fetch remote features (useful when feature tags like :latest are updated)")
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Create environment service and delegate to it
	svc := service.NewEnvironmentService(dockerClient, workspacePath, configPath, verbose)

	return svc.Build(ctx, service.BuildOptions{
		NoCache: noCache,
		Pull:    pullBuild,
	})
}
