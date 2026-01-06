package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
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
	fmt.Printf("Current version: %s\n", Version)

	// Get latest release info
	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := release.TagName
	fmt.Printf("Latest version:  %s\n", latestVersion)

	// Compare versions (strip 'v' prefix for comparison)
	currentClean := strings.TrimPrefix(Version, "v")
	latestClean := strings.TrimPrefix(latestVersion, "v")

	if currentClean == latestClean {
		fmt.Println("Already up to date!")
		return nil
	}

	if Version == "dev" {
		fmt.Println("Running development version, upgrading to latest release...")
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

	fmt.Printf("Downloading %s...\n", binaryName)

	// Download to temp file
	tmpFile, err := os.CreateTemp("", "dcx-upgrade-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	resp, err := http.Get(downloadURL)
	if err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		tmpFile.Close()
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	_, err = io.Copy(tmpFile, resp.Body)
	tmpFile.Close()
	if err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("failed to set permissions: %w", err)
	}

	// Get current executable path
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Replace current executable
	fmt.Printf("Installing to %s...\n", execPath)

	if err := os.Rename(tmpPath, execPath); err != nil {
		// Try copy if rename fails (cross-device)
		if err := copyFile(tmpPath, execPath); err != nil {
			return fmt.Errorf("failed to install: %w", err)
		}
	}

	fmt.Printf("Successfully upgraded to %s!\n", latestVersion)
	fmt.Printf("Release notes: %s\n", release.HTMLURL)

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

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	dest, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dest.Close()

	_, err = io.Copy(dest, source)
	if err != nil {
		return err
	}

	return os.Chmod(dst, 0755)
}
