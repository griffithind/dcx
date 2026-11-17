package cli

import (
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	lockUpdate bool
	lockVerify bool
	lockFrozen bool
	lockJSON   bool
)

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Generate or verify devcontainer-lock.json",
	Long: `Generate or verify a lockfile that pins exact feature versions for reproducible builds.

The lockfile has the official devcontainer CLI's format. For each feature,
keyed by its reference as written, it records:
- Exact semantic version resolved
- The resolved reference, pinned to the OCI manifest digest
- Integrity: the manifest digest (or a tarball's SHA256)
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
	lockCmd.Flags().BoolVar(&lockUpdate, "update", false, "")
	_ = lockCmd.Flags().MarkDeprecated("update", "it is the default; run 'dcx lock'")
	lockCmd.Flags().BoolVar(&lockVerify, "verify", false, "verify lockfile matches resolved features without updating")
	lockCmd.Flags().BoolVar(&lockFrozen, "frozen", false, "fail if lockfile doesn't exist or doesn't match (CI mode)")
	// The flags select one mode; together they used to cancel out
	// (doctor then checked nothing and reported success).
	lockCmd.MarkFlagsMutuallyExclusive("verify", "frozen", "update")
	lockCmd.Flags().BoolVar(&lockJSON, "json", false, "print the result as machine-readable JSON")
	lockCmd.GroupID = "maintenance"
	rootCmd.AddCommand(lockCmd)
}

func runLock(cmd *cobra.Command, args []string) error {
	// Locking resolves features from registries; it needs no Docker daemon.
	ctx, stop := commandContext()
	defer stop()
	rep := ui.Reporter()
	if lockJSON {
		rep = report.Discard
	}
	svc := newService(newEngine(), rep)

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
	spinner := &ui.Spinner{}
	if !lockJSON {
		spinner = ui.StartSpinner(spinnerMsg)
	}

	// Execute lock operation
	result, err := svc.Lock(ctx, service.LockOptions{
		Mode: mode,
	})

	// The error itself is printed once, by Execute.
	if err != nil {
		spinner.Stop()
		return err
	}

	if lockJSON {
		changes := result.Changes
		if changes == nil {
			changes = []string{}
		}
		return printJSON(lockResultJSON{
			Action: lockActionName(result.Action), LockfilePath: result.LockfilePath,
			FeatureCount: result.FeatureCount, Changes: changes,
		})
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

// lockResultJSON is the lock --json output.
type lockResultJSON struct {
	Action       string   `json:"action"` // created, updated, verified, unchanged or no-features
	LockfilePath string   `json:"lockfilePath"`
	FeatureCount int      `json:"featureCount"`
	Changes      []string `json:"changes"`
}

func lockActionName(a service.LockAction) string {
	switch a {
	case service.LockActionCreated:
		return "created"
	case service.LockActionUpdated:
		return "updated"
	case service.LockActionVerified:
		return "verified"
	case service.LockActionNoChange:
		return "unchanged"
	case service.LockActionNoFeatures:
		return "no-features"
	}
	return "unknown"
}
