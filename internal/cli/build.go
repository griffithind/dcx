package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/griffithind/dcx/internal/compose"
	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/runner"
	"github.com/griffithind/dcx/internal/state"
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

	// Parse devcontainer configuration
	cfg, cfgPath, err := config.Load(workspacePath, configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if verbose {
		fmt.Printf("Loaded configuration from: %s\n", cfgPath)
	}

	// Load dcx.json configuration (optional)
	dcxCfg, err := config.LoadDcxConfig(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to load dcx.json: %w", err)
	}

	// Get project name from dcx.json
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = state.SanitizeProjectName(dcxCfg.Name)
		if verbose {
			fmt.Printf("Project name: %s\n", projectName)
		}
	}

	// Validate configuration
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Compute identifiers
	envKey := state.ComputeEnvKey(workspacePath)
	configHash, err := config.ComputeHash(cfg)
	if err != nil {
		return fmt.Errorf("failed to compute config hash: %w", err)
	}

	if verbose {
		fmt.Printf("Env key: %s\n", envKey)
		fmt.Printf("Config hash: %s\n", configHash[:12])
	}

	// Build based on plan type
	if cfg.IsComposePlan() {
		return buildCompose(ctx, dockerClient, cfg, cfgPath, projectName, envKey, configHash)
	}

	if cfg.IsSinglePlan() {
		return buildSingle(ctx, dockerClient, cfg, cfgPath, envKey, configHash)
	}

	return fmt.Errorf("invalid configuration: no build plan detected")
}

func buildCompose(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, cfgPath, projectName, envKey, configHash string) error {
	fmt.Println("Building compose-based environment...")

	// Create compose runner with docker client for API operations
	composeRunner, err := compose.NewRunner(dockerClient, workspacePath, cfgPath, cfg, projectName, envKey, configHash)
	if err != nil {
		return fmt.Errorf("failed to create compose runner: %w", err)
	}

	// Run compose build
	if err := composeRunner.Build(ctx, runner.BuildOptions{
		NoCache: noCache,
		Pull:    pullBuild,
	}); err != nil {
		return fmt.Errorf("failed to build: %w", err)
	}

	fmt.Println("Build complete")
	return nil
}

func buildSingle(ctx context.Context, dockerClient *docker.Client, cfg *config.DevcontainerConfig, cfgPath, envKey, configHash string) error {
	// Image-based: just pull the image
	if cfg.Image != "" {
		fmt.Printf("Pulling image: %s\n", cfg.Image)

		// Check if image exists locally
		exists, err := dockerClient.ImageExists(ctx, cfg.Image)
		if err != nil {
			return fmt.Errorf("failed to check image: %w", err)
		}

		if exists && !noCache {
			fmt.Println("Image already exists locally")
			return nil
		}

		// Pull the image
		if err := dockerClient.PullImage(ctx, cfg.Image); err != nil {
			return fmt.Errorf("failed to pull image: %w", err)
		}

		fmt.Println("Pull complete")
		return nil
	}

	// Dockerfile-based: build the image
	if cfg.Build != nil {
		imageTag := fmt.Sprintf("dcx/%s:%s", envKey, configHash[:12])
		fmt.Printf("Building image: %s\n", imageTag)

		buildOpts := docker.BuildOptions{
			Tag:        imageTag,
			Dockerfile: cfg.Build.Dockerfile,
			Context:    cfg.Build.Context,
			Args:       cfg.Build.Args,
			Target:     cfg.Build.Target,
			CacheFrom:  cfg.Build.CacheFrom,
			ConfigDir:  cfgPath,
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
		}

		if err := dockerClient.BuildImage(ctx, buildOpts); err != nil {
			return fmt.Errorf("failed to build image: %w", err)
		}

		fmt.Println("Build complete")
		return nil
	}

	return fmt.Errorf("no image or build configuration found")
}
