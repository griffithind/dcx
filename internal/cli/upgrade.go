package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"

	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"

	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/version"
)

const (
	repoOwner = "griffithind"
	repoName  = "dcx"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade dcx to the latest version",
	Long: `Check for and install the latest version of dcx from GitHub releases.

The binary will be replaced in-place. If the current version is already
the latest, no action is taken.`,
	RunE: runUpgrade,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	out := output.Global()
	c := out.Color()

	out.Printf("Current version: %s", c.Code(version.Version))

	// Get latest release info
	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := release.TagName
	out.Printf("Latest version:  %s", c.Code(latestVersion))

	// Compare versions (strip 'v' prefix for comparison)
	currentClean := strings.TrimPrefix(version.Version, "v")
	latestClean := strings.TrimPrefix(latestVersion, "v")

	if currentClean == latestClean {
		out.Println(output.FormatSuccess("Already up to date!"))
		return nil
	}

	if version.Version == "dev" {
		out.Println(c.Warning("Running development version, upgrading to latest release..."))
	}

	// Determine binary name for this platform
	binaryName := fmt.Sprintf("dcx-%s-%s", runtime.GOOS, runtime.GOARCH)

	// Find download URL
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.Name == binaryName {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		return fmt.Errorf("no binary available for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	// Start spinner for download
	spinner := output.NewSpinner(fmt.Sprintf("Downloading %s...", binaryName))
	if !out.IsQuiet() && !out.IsJSON() {
		spinner.Start()
	}

	resp, err := http.Get(downloadURL)
	if err != nil {
		spinner.StopWithError("Download failed")
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		spinner.StopWithError("Download failed")
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Apply the update using selfupdate library
	// This handles "text file busy" and other OS-specific issues
	if err := selfupdate.Apply(resp.Body, selfupdate.Options{}); err != nil {
		spinner.StopWithError("Update failed")
		return fmt.Errorf("failed to apply update: %w", err)
	}

	if !out.IsQuiet() && !out.IsJSON() {
		spinner.StopWithSuccess(fmt.Sprintf("Successfully upgraded to %s!", latestVersion))
	}

	out.Printf("Release notes: %s", c.Code(release.HTMLURL))

	return nil
}

func getLatestRelease() (*githubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", repoOwner, repoName)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

