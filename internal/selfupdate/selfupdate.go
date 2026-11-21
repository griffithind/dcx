// Package selfupdate replaces the running dcx with the latest GitHub
// release, with the same checks as install.sh and one more: the release is
// found with the GitHub API, the binary for this OS and architecture is
// downloaded over HTTPS and checked against the release's checksums.txt,
// the new binary must report the release's version, and only then does it
// replace the old one, in one rename in the same directory (a failure at
// any step leaves the installed dcx as it was).
package selfupdate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const repo = "griffithind/dcx"

// maxBinarySize bounds the download (dcx is a few tens of MiB).
const maxBinarySize = 256 << 20

// Updater finds, downloads and installs releases. New returns the one for
// GitHub; tests point the URLs at a fake.
type Updater struct {
	Client    *http.Client
	API       string // GitHub API base URL
	Downloads string // release downloads base URL
	GOOS      string
	GOARCH    string
}

// New returns the Updater for dcx's GitHub releases and this platform.
func New() *Updater {
	return &Updater{
		Client: &http.Client{
			Timeout: 5 * time.Minute,
			// GitHub redirects downloads to its storage host; never
			// follow a redirect from HTTPS to plain HTTP.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
					return fmt.Errorf("refusing redirect to %s", req.URL.Redacted())
				}
				if len(via) >= 10 {
					return errors.New("too many redirects")
				}
				return nil
			},
		},
		API:       "https://api.github.com",
		Downloads: "https://github.com",
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
}

// release matches the tags dcx releases are published under. Anything
// else, such as a source build's `git describe` ("v0.8.0-3-gabc1234"), is
// not a release.
var release = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// IsRelease reports whether v is a release version (vMAJOR.MINOR.PATCH).
func IsRelease(v string) bool { return release.MatchString(v) }

// Newer reports whether release version latest is newer than current.
// Both must be release versions.
func Newer(latest, current string) bool {
	l, c := release.FindStringSubmatch(latest), release.FindStringSubmatch(current)
	if l == nil || c == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		ln, _ := strconv.Atoi(l[i])
		cn, _ := strconv.Atoi(c[i])
		if ln != cn {
			return ln > cn
		}
	}
	return false
}

// Latest returns the tag of the latest release.
func (u *Updater) Latest(ctx context.Context) (string, error) {
	data, err := u.fetch(ctx, u.API+"/repos/"+repo+"/releases/latest", 4<<20)
	if err != nil {
		return "", fmt.Errorf("failed to look up the latest release: %w", err)
	}
	var r struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", fmt.Errorf("failed to look up the latest release: %w", err)
	}
	if !IsRelease(r.TagName) {
		return "", fmt.Errorf("the latest release tag %q is not a version", r.TagName)
	}
	return r.TagName, nil
}

// AssetName is the release asset for this platform ("dcx-linux-amd64").
func (u *Updater) AssetName() string { return "dcx-" + u.GOOS + "-" + u.GOARCH }

// Download returns the binary for this platform from release tag, checked
// against the release's checksums.txt.
func (u *Updater) Download(ctx context.Context, tag string) ([]byte, error) {
	if !IsRelease(tag) {
		return nil, fmt.Errorf("%q is not a release version", tag)
	}
	name := u.AssetName()
	checksums, err := u.fetch(ctx, u.assetURL(tag, "checksums.txt"), 1<<20)
	if err != nil {
		return nil, fmt.Errorf("failed to download checksums.txt of %s: %w", tag, err)
	}
	want, err := checksumFor(checksums, name)
	if err != nil {
		return nil, err
	}
	binary, err := u.fetch(ctx, u.assetURL(tag, name), maxBinarySize)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s of %s: %w", name, tag, err)
	}
	if got := sha256.Sum256(binary); !bytes.Equal(got[:], want) {
		return nil, fmt.Errorf("%s does not match checksums.txt (expected %x, got %x); not installing it", name, want, got)
	}
	return binary, nil
}

func (u *Updater) assetURL(tag, name string) string {
	return u.Downloads + "/" + repo + "/releases/download/" + url.PathEscape(tag) + "/" + url.PathEscape(name)
}

// checksumFor finds name's SHA-256 in sha256sum output.
func checksumFor(checksums []byte, name string) ([]byte, error) {
	scanner := bufio.NewScanner(bytes.NewReader(checksums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			sum, err := hex.DecodeString(fields[0])
			if err != nil || len(sum) != sha256.Size {
				return nil, fmt.Errorf("checksums.txt has a malformed checksum for %s", name)
			}
			return sum, nil
		}
	}
	return nil, fmt.Errorf("checksums.txt has no entry for %s", name)
}

// fetch GETs url, failing on a status other than 200 or a body over limit
// bytes.
func (u *Updater) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := u.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // nothing to do about a close error after reading
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", url, limit)
	}
	return data, nil
}

// Executable returns the path of the running dcx, symlinks resolved, which
// Install replaces.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Install replaces the binary at path with binary, which must report
// version tag ("dcx version <tag>"). The new binary is written next to the
// old one and renamed over it, so the replacement is atomic and a running
// dcx keeps its old file.
func Install(ctx context.Context, path string, binary []byte, tag string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".dcx-upgrade-*")
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Errorf("cannot replace %s: %s is not writable by you (run 'sudo dcx upgrade', or the install command)", path, dir)
		}
		return fmt.Errorf("cannot replace %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) //nolint:errcheck // gone after the rename
	_, err = tmp.Write(binary)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmpPath, 0o755)
	}
	if err != nil {
		return fmt.Errorf("failed to write the new dcx next to %s: %w", path, err)
	}
	if err := checkVersion(ctx, tmpPath, tag); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("cannot replace %s: %w", path, err)
	}
	return nil
}

// checkVersion runs the new binary once: it must start and report tag.
func checkVersion(ctx context.Context, binary, tag string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		return fmt.Errorf("the downloaded dcx does not run here (%v); not installing it", err)
	}
	if got, want := strings.TrimSpace(string(out)), "dcx version "+tag; got != want {
		return fmt.Errorf("the downloaded dcx reports %q, expected %q; not installing it", got, want)
	}
	return nil
}
