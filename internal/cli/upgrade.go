package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/selfupdate"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/griffithind/dcx/internal/version"
	"github.com/spf13/cobra"
)

var upgradeCheck bool

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade dcx to the latest release",
	Long: `Replace this dcx with the latest release from GitHub.

The binary for this OS and architecture is downloaded over HTTPS and
checked against the release's checksums.txt, and must report the release's
version when run; only then does it replace this dcx, in one rename (a
failure leaves this dcx as it was). Nothing happens when this dcx is the
latest release. A dcx built from source is not replaced: rebuild it, or
install a release with the install command.`,
	Args: cobra.NoArgs,
	RunE: runUpgrade,
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeCheck, "check", false, "only report whether a newer release exists")
	upgradeCmd.GroupID = "maintenance"
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	ctx, stop := commandContext() // Ctrl-C cancels a download
	defer stop()
	current := version.Version
	if !selfupdate.IsRelease(current) {
		return fmt.Errorf("this dcx was built from source (%s); rebuild it, or install a release with the install command", current)
	}
	u := selfupdate.New()
	latest, err := u.Latest(ctx)
	if err != nil {
		return err
	}
	if !selfupdate.Newer(latest, current) {
		ui.Success("dcx %s is up to date (latest release: %s)", current, latest)
		return nil
	}
	if upgradeCheck {
		ui.Info("dcx %s is available (this is %s); run 'dcx upgrade'", latest, current)
		return nil
	}
	path, err := selfupdate.Executable()
	if err != nil {
		return fmt.Errorf("cannot find this dcx's binary: %w", err)
	}

	spinner := ui.StartSpinner(fmt.Sprintf("Downloading dcx %s (%s)...", latest, u.AssetName()))
	binary, err := u.Download(ctx, latest)
	if err == nil {
		err = selfupdate.Install(ctx, path, binary, latest)
	}
	spinner.Stop()
	if err != nil {
		return err
	}
	ui.Success("Upgraded dcx %s to %s (%s)", current, latest, path)
	ui.Info("Release notes: https://github.com/griffithind/dcx/releases/tag/%s", latest)
	return nil
}
