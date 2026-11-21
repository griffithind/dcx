package devcontainer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResourceName(t *testing.T) {
	assert.Equal(t, "my_app-abcdefgh", ResourceName("My App", "abcdefghijkl"))
	assert.Equal(t, "dcx-abcdefgh", ResourceName("!!!", "abcdefghijkl"))
	assert.Equal(t, "dcx_1st-abc", ResourceName("1st", "abc"))
}

// Two checkouts with the same devcontainer "name" must not share Docker
// resource names.
func TestResourceNameUniquePerWorkspace(t *testing.T) {
	a := ResourceName("Go", ComputeID(filepath.Join(t.TempDir(), "clone-a")))
	b := ResourceName("Go", ComputeID(filepath.Join(t.TempDir(), "clone-b")))
	assert.NotEqual(t, a, b)
}

// Two configs of one folder are two environments (F8); the default
// locations keep the path-only ID, so existing workspaces keep theirs.
func TestWorkspaceIDDependsOnNonDefaultConfig(t *testing.T) {
	ws := t.TempDir()
	base := ComputeID(ws)
	for _, cfg := range []string{"", filepath.Join(ws, ".devcontainer", "devcontainer.json"), filepath.Join(ws, ".devcontainer.json")} {
		if got := WorkspaceID(ws, cfg); got != base {
			t.Errorf("WorkspaceID(%q) = %s, want the path-only ID %s", cfg, got, base)
		}
	}
	a := WorkspaceID(ws, filepath.Join(ws, ".devcontainer", "a", "devcontainer.json"))
	b := WorkspaceID(ws, filepath.Join(ws, ".devcontainer", "b", "devcontainer.json"))
	if a == base || b == base || a == b {
		t.Errorf("per-config IDs must differ: base=%s a=%s b=%s", base, a, b)
	}
	if again := WorkspaceID(ws, filepath.Join(ws, ".devcontainer", "a", "devcontainer.json")); again != a {
		t.Error("per-config IDs must be stable")
	}
}

// A workspace reached through a symlink (macOS's /var is /private/var)
// keeps the path-only ID for a default config path that does not exist
// yet: the config path resolves under the workspace's resolved folder.
func TestWorkspaceIDResolvesMissingConfigUnderSymlink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	cfg := filepath.Join(link, ".devcontainer", "devcontainer.json")
	if got, want := WorkspaceID(link, cfg), ComputeID(link); got != want {
		t.Errorf("WorkspaceID through a symlink = %s, want the path-only ID %s", got, want)
	}
}
