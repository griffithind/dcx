package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpgradeDecision(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		force, proceed  bool
		reason          string
	}{
		{"v1.2.3", "v1.3.0", false, true, ""},
		{"1.2.3", "v1.2.4", false, true, ""},
		{"v1.2.3", "v1.2.3", false, false, "Already up to date"},
		{"v1.2.3", "v1.2.3", true, true, "Reinstalling"},
		{"v1.10.0", "v1.9.0", false, false, "newer than the latest release"},
		{"v1.10.0", "v1.9.0", true, true, "Downgrading"},
		{"v1.3.0-rc1", "v1.3.0", false, true, ""},
		{"v1.3.0", "v1.3.0-rc1", false, false, "newer than the latest release"},
		{"dev", "v1.3.0", false, false, "development build"},
		{"dev", "v1.3.0", true, true, "Replacing development build"},
		{"v1.2.3", "nightly", true, false, "not a version"},
	} {
		t.Run(fmt.Sprintf("%s->%s force=%v", tc.current, tc.latest, tc.force), func(t *testing.T) {
			proceed, reason := upgradeDecision(tc.current, tc.latest, tc.force)
			assert.Equal(t, tc.proceed, proceed)
			if tc.reason == "" {
				assert.Empty(t, reason)
			} else {
				assert.Contains(t, reason, tc.reason)
			}
		})
	}
}

// fakeReleaseServer serves a release with a binary and a checksums.txt
// listing sum for it.
func fakeReleaseServer(t *testing.T, binary []byte, checksums string) *githubRelease {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bin":
			_, _ = w.Write(binary)
		case "/checksums.txt":
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	rel := &githubRelease{TagName: "v9.9.9"}
	rel.Assets = append(rel.Assets,
		struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{"dcx-test-os", srv.URL + "/bin"},
		struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		}{"checksums.txt", srv.URL + "/checksums.txt"},
	)
	return rel
}

func TestDownloadVerified(t *testing.T) {
	binary := []byte("new dcx binary")
	sum := sha256.Sum256(binary)
	good := fmt.Sprintf("%x  dcx-other\n%x  dcx-test-os\n", sha256.Sum256([]byte("x")), sum)

	got, gotSum, err := downloadVerified(context.Background(), fakeReleaseServer(t, binary, good), "dcx-test-os")
	require.NoError(t, err)
	assert.Equal(t, binary, got)
	assert.Equal(t, sum[:], gotSum)

	tampered := fakeReleaseServer(t, []byte("tampered"), good)
	_, _, err = downloadVerified(context.Background(), tampered, "dcx-test-os")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match checksums.txt")

	_, _, err = downloadVerified(context.Background(), fakeReleaseServer(t, binary, "abc  dcx-other\n"), "dcx-test-os")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no entry for dcx-test-os")

	_, _, err = downloadVerified(context.Background(), fakeReleaseServer(t, binary, good), "dcx-missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "has no binary")
}
