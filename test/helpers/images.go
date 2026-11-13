package helpers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/griffithind/dcx/internal/devcontainer"
)

var (
	workspacesMu sync.Mutex
	workspaceIDs = map[string]bool{}
)

// registerWorkspace records a directory dcx ran in so RemoveTestImages can
// delete the images dcx built for it (dcx/<workspace id>:*). Only
// directories under the OS temp dir are recorded: a test running dcx in a
// real checkout must never cause that checkout's images to be removed.
func registerWorkspace(dir string) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		tmp = os.TempDir()
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	if !strings.HasPrefix(abs, tmp+string(filepath.Separator)) {
		return
	}
	workspacesMu.Lock()
	defer workspacesMu.Unlock()
	workspaceIDs[devcontainer.ComputeID(abs)] = true
}

// RemoveTestImages deletes the dcx-built images of every workspace the
// tests created, so e2e runs don't accumulate dcx/<id>:* images on the
// developer's machine. Images of the developer's own workspaces are never
// touched: only IDs of temporary test directories are listed. Call it from
// TestMain after m.Run.
func RemoveTestImages() {
	workspacesMu.Lock()
	ids := make([]string, 0, len(workspaceIDs))
	for id := range workspaceIDs {
		ids = append(ids, id)
	}
	workspacesMu.Unlock()
	for _, id := range ids {
		out, err := exec.Command("docker", "image", "ls", "-q", "--filter", "reference=dcx/"+id+":*").Output()
		if err != nil {
			continue
		}
		images := strings.Fields(string(out))
		if len(images) == 0 {
			continue
		}
		_ = exec.Command("docker", append([]string{"image", "rm", "-f"}, images...)...).Run()
	}
}
