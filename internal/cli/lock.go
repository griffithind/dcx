package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	lockUpdate bool
	lockVerify bool
	lockFrozen bool
)

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Generate or verify devcontainer-lock.json",
	Long: `Generate or verify a lockfile that pins exact feature versions for reproducible builds.

The lockfile records for each feature:
- Exact semantic version resolved
- OCI manifest digest for content-addressable references
- SHA256 integrity hash for verification
- Hard dependencies (dependsOn)

Modes:
  dcx lock           Generate/update lockfile (default)
  dcx lock --verify  Verify existing lockfile matches resolved features
  dcx lock --frozen  Fail if lockfile doesn't exist or doesn't match (CI mode)

Per the devcontainer specification:
- .devcontainer.json → .devcontainer-lock.json
- devcontainer.json → devcontainer-lock.json
- Local features (./path) are excluded from lockfile`,
	RunE: runLock,
}

func init() {
	lockCmd.Flags().BoolVar(&lockUpdate, "update", false, "update existing lockfile with new features (same as no flags)")
	lockCmd.Flags().BoolVar(&lockVerify, "verify", false, "verify lockfile matches resolved features without updating")
	lockCmd.Flags().BoolVar(&lockFrozen, "frozen", false, "fail if lockfile doesn't exist or doesn't match (CI mode)")
	lockCmd.GroupID = "maintenance"
	rootCmd.AddCommand(lockCmd)
}

func runLock(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := container.NewDockerClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Create devcontainer service
	svc := service.NewDevContainerService(dockerClient, workspacePath, configPath, verbose)
	defer svc.Close()

	// Determine lock mode
	mode := service.LockModeGenerate
	if lockVerify {
		mode = service.LockModeVerify
	} else if lockFrozen {
		mode = service.LockModeFrozen
	}

	// Start spinner for progress feedback
	spinnerMsg := "Generating lockfile..."
	if lockVerify {
		spinnerMsg = "Verifying lockfile..."
	} else if lockFrozen {
		spinnerMsg = "Checking lockfile..."
	}
	spinner := ui.StartSpinner(spinnerMsg)

	// Execute lock operation
	result, err := svc.Lock(ctx, service.LockOptions{
		Mode: mode,
	})

	// Stop spinner with appropriate message
	if err != nil {
		spinner.Fail("Lockfile operation failed")
		return err
	}

	switch result.Action {
	case service.LockActionCreated:
		spinner.Success("Created lockfile")
		ui.Printf("  Path: %s", result.LockfilePath)
		ui.Printf("  Features: %d", result.FeatureCount)

	case service.LockActionUpdated:
		spinner.Success("Updated lockfile")
		ui.Printf("  Path: %s", result.LockfilePath)
		ui.Printf("  Features: %d", result.FeatureCount)
		if len(result.Changes) > 0 {
			ui.Println("  Changes:")
			for _, change := range result.Changes {
				ui.Printf("    - %s", change)
			}
		}

	case service.LockActionVerified:
		spinner.Success("Lockfile is up to date")
		ui.Printf("  Path: %s", result.LockfilePath)
		ui.Printf("  Features: %d", result.FeatureCount)

	case service.LockActionNoChange:
		spinner.Success("Lockfile is already up to date")
		ui.Printf("  Path: %s", result.LockfilePath)

	case service.LockActionNoFeatures:
		spinner.Success("No features to lock")
		ui.Println("  This devcontainer has no features defined")
	}

	return nil
}
