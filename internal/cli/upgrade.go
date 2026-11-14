package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/ui"
	"github.com/griffithind/dcx/internal/version"
	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"
)

const (
	repoOwner = "griffithind"
	repoName  = "dcx"

	// maxBinarySize bounds the download (dcx is a few tens of MiB).
	maxBinarySize = 256 << 20
)

// githubAPI is the GitHub API base URL (tests point it at a fake).
var githubAPI = "https://api.github.com"

// upgradeClient bounds every upgrade request, so a stalled connection
// fails instead of hanging.
var upgradeClient = &http.Client{Timeout: 5 * time.Minute}

var upgradeForce bool

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade dcx to the latest version",
	Long: `Check for and install the latest release of dcx from GitHub.

The binary is verified against the release's checksums.txt and replaced in
place. Nothing happens when this dcx is the latest release or newer; a
development build or a newer build is replaced only with --force, which
also reinstalls the current release.`,
	Args: cobra.NoArgs,
	RunE: runUpgrade,
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeForce, "force", false, "install the latest release even over a development, newer or identical build")
	upgradeCmd.GroupID = "maintenance"
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

// assetURL returns the download URL of the named asset, "" when absent.
func (r *githubRelease) assetURL(name string) string {
	for _, a := range r.Assets {
		if a.Name == name {
			return a.BrowserDownloadURL
		}
	}
	return ""
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	ctx, stop := commandContext() // Ctrl-C cancels a download
	defer stop()
	ui.Info("Current version: %s", ui.Code(version.Version))

	release, err := getLatestRelease(ctx)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	ui.Info("Latest version:  %s", ui.Code(release.TagName))

	if proceed, reason := upgradeDecision(version.Version, release.TagName, upgradeForce); !proceed {
		ui.Success("%s", reason)
		return nil
	} else if reason != "" {
		ui.Warning("%s", reason)
	}

	binaryName := fmt.Sprintf("dcx-%s-%s", runtime.GOOS, runtime.GOARCH)
	spinner := ui.StartSpinner(fmt.Sprintf("Downloading %s...", binaryName))
	binary, sum, err := downloadVerified(ctx, release, binaryName)
	if err != nil {
		spinner.Fail("Download failed")
		return err
	}

	// selfupdate checks the checksum again and handles "text file busy"
	// and other OS-specific issues of replacing a running binary.
	if err := selfupdate.Apply(bytes.NewReader(binary), selfupdate.Options{Checksum: sum}); err != nil {
		spinner.Fail("Update failed")
		return fmt.Errorf("failed to apply update: %w", err)
	}
	spinner.Success(fmt.Sprintf("Successfully upgraded to %s!", release.TagName))
	ui.Info("Release notes: %s", ui.Code(release.HTMLURL))
	return nil
}

// upgradeDecision says whether to install latest over current, and why
// (a reason to report when it does not, a warning when it does under
// force).
func upgradeDecision(current, latest string, force bool) (bool, string) {
	cur, curOK := parseVersion(current)
	lat, latOK := parseVersion(latest)
	switch {
	case !latOK:
		return false, fmt.Sprintf("The latest release tag %q is not a version; not upgrading.", latest)
	case !curOK && force:
		return true, fmt.Sprintf("Replacing development build %s with %s.", current, latest)
	case !curOK:
		return false, fmt.Sprintf("This is a development build (%s); run 'dcx upgrade --force' to replace it with %s.", current, latest)
	}
	switch c := compareVersions(cur, lat); {
	case c == 0 && force:
		return true, fmt.Sprintf("Reinstalling %s.", latest)
	case c == 0:
		return false, "Already up to date!"
	case c > 0 && force:
		return true, fmt.Sprintf("Downgrading from %s to %s.", current, latest)
	case c > 0:
		return false, fmt.Sprintf("This build (%s) is newer than the latest release (%s); run 'dcx upgrade --force' to downgrade.", current, latest)
	}
	return true, ""
}

// semVersion is a parsed vMAJOR.MINOR.PATCH[-PRERELEASE][+BUILD].
type semVersion struct {
	nums [3]int
	pre  string
}

// parseVersion parses a semantic version with an optional "v" prefix;
// ok is false for anything else ("dev", a commit hash).
func parseVersion(s string) (v semVersion, ok bool) {
	s = strings.TrimPrefix(s, "v")
	s, _, _ = strings.Cut(s, "+")
	s, v.pre, _ = strings.Cut(s, "-")
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.nums[i] = n
	}
	return v, true
}

// compareVersions orders versions as semver does, except that pre-release
// identifiers compare as plain strings.
func compareVersions(a, b semVersion) int {
	for i := range a.nums {
		if a.nums[i] != b.nums[i] {
			if a.nums[i] < b.nums[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case a.pre == b.pre:
		return 0
	case a.pre == "": // a release sorts after its pre-releases
		return 1
	case b.pre == "":
		return -1
	}
	return strings.Compare(a.pre, b.pre)
}

// downloadVerified downloads the release asset name and checks it against
// the release's checksums.txt, returning the binary and its SHA-256.
func downloadVerified(ctx context.Context, release *githubRelease, name string) ([]byte, []byte, error) {
	binaryURL := release.assetURL(name)
	if binaryURL == "" {
		return nil, nil, fmt.Errorf("release %s has no binary for %s/%s", release.TagName, runtime.GOOS, runtime.GOARCH)
	}
	checksumsURL := release.assetURL("checksums.txt")
	if checksumsURL == "" {
		return nil, nil, fmt.Errorf("release %s has no checksums.txt; not installing an unverified binary", release.TagName)
	}
	checksums, err := fetch(ctx, checksumsURL, 1<<20)
	if err != nil {
		return nil, nil, fmt.Errorf("download checksums.txt: %w", err)
	}
	want, err := checksumFor(checksums, name)
	if err != nil {
		return nil, nil, err
	}
	binary, err := fetch(ctx, binaryURL, maxBinarySize)
	if err != nil {
		return nil, nil, fmt.Errorf("download %s: %w", name, err)
	}
	got := sha256.Sum256(binary)
	if !bytes.Equal(got[:], want) {
		return nil, nil, fmt.Errorf("%s does not match checksums.txt (expected %x, got %x); not installing it", name, want, got)
	}
	return binary, want, nil
}

// checksumFor finds name's SHA-256 in sha256sum output.
func checksumFor(checksums []byte, name string) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(checksums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			sum, err := hex.DecodeString(fields[0])
			if err != nil || len(sum) != sha256.Size {
				return nil, fmt.Errorf("checksums.txt: malformed checksum for %s", name)
			}
			return sum, nil
		}
	}
	return nil, fmt.Errorf("checksums.txt has no entry for %s", name)
}

// fetch GETs url (cancelled with ctx), failing on a non-200 status or a
// body over limit bytes.
func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := upgradeClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // Close error irrelevant after read
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	return data, nil
}

func getLatestRelease(ctx context.Context) (*githubRelease, error) {
	data, err := fetch(ctx, fmt.Sprintf("%s/repos/%s/%s/releases/latest", githubAPI, repoOwner, repoName), 4<<20)
	if err != nil {
		return nil, fmt.Errorf("GitHub API: %w", err)
	}
	var release githubRelease
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, err
	}
	return &release, nil
}
