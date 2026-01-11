package cli

import (
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ui"
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
	buildCmd.GroupID = "maintenance"
	rootCmd.AddCommand(buildCmd)
}

func runBuild(cmd *cobra.Command, args []string) error {
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	// Start spinner for progress feedback
	spinner := ui.StartSpinner("Building devcontainer images...")

	// Execute build
	buildErr := cliCtx.Service.Build(cliCtx.Ctx, service.BuildOptions{
		NoCache: noCache,
		Pull:    pullBuild,
	})

	// Stop spinner with appropriate message
	if buildErr != nil {
		spinner.Fail("Failed to build devcontainer images")
	} else {
		spinner.Success("Build completed successfully")
	}

	return buildErr
}
