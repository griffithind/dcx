package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	cleanAll      bool
	cleanDangling bool
	dryRun        bool
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Clean up orphaned resources",
	Long: `Clean up orphaned dcx resources.

This command removes:
- Derived images created by dcx (dcx-derived/*)
- Optionally, dangling (untagged) images

By default, only derived images are cleaned. Use --all to include dangling images.

This is an offline-safe command that does not require network access.`,
	RunE: runClean,
}

func init() {
	cleanCmd.Flags().BoolVar(&cleanAll, "all", false, "also clean dangling images")
	cleanCmd.Flags().BoolVar(&cleanDangling, "dangling", false, "only clean dangling images")
	cleanCmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be cleaned without removing")
	cleanCmd.GroupID = "maintenance"
	rootCmd.AddCommand(cleanCmd)
}

func runClean(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client (uses singleton)
	docker, err := container.DockerClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}

	if dryRun {
		return showCleanStats(ctx, docker)
	}

	var totalImages int
	var totalSpace int64

	// Clean derived images (unless only dangling is requested)
	if !cleanDangling {
		ui.Println("Cleaning derived images...")
		result, err := docker.CleanupAllDerivedImages(ctx)
		if err != nil {
			return fmt.Errorf("failed to clean derived images: %w", err)
		}

		if result.ImagesRemoved > 0 {
			ui.Printf("  Removed %d derived image(s), reclaimed %s", result.ImagesRemoved, formatBytes(result.SpaceReclaimed))
		} else {
			ui.Println("  No derived images to clean")
		}

		totalImages += result.ImagesRemoved
		totalSpace += result.SpaceReclaimed
	}

	// Clean dangling images if requested
	if cleanAll || cleanDangling {
		ui.Println("Cleaning dangling images...")
		result, err := docker.CleanupDanglingImages(ctx)
		if err != nil {
			return fmt.Errorf("failed to clean dangling images: %w", err)
		}

		if result.ImagesRemoved > 0 {
			ui.Printf("  Removed %d dangling image(s), reclaimed %s", result.ImagesRemoved, formatBytes(result.SpaceReclaimed))
		} else {
			ui.Println("  No dangling images to clean")
		}

		totalImages += result.ImagesRemoved
		totalSpace += result.SpaceReclaimed
	}

	ui.Println("")
	if totalImages > 0 {
		ui.Success("Total: %d image(s) removed, %s reclaimed", totalImages, formatBytes(totalSpace))
	} else {
		ui.Println("Nothing to clean")
	}

	return nil
}

func showCleanStats(ctx context.Context, docker *container.Docker) error {
	ui.Println(ui.Bold("Dry run - showing what would be cleaned:"))
	ui.Println("")

	// Show derived images
	count, size, err := docker.GetDerivedImageStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get derived image stats: %w", err)
	}

	if count > 0 {
		ui.Printf("Derived images: %d (%s)", count, formatBytes(size))
	} else {
		ui.Println("Derived images: none")
	}

	return nil
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)

	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
