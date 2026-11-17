// Package dcxhome locates the directory where dcx keeps its state on the
// host: known_hosts and the fallback client key, and one folder per
// workspace (WorkspaceDir) with its host key, lock, compose override and
// record. Older dcx versions kept the per-workspace files in hostkeys/,
// locks/ and compose/; they are moved or removed (see LegacyDirs).
package dcxhome

import (
	"os"
	"path/filepath"
)

// Env relocates the directory (tests use it to stay out of the user's).
const Env = "DCX_HOME"

// Dir returns $DCX_HOME, or ~/.dcx.
func Dir() (string, error) {
	if dir := os.Getenv(Env); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".dcx"), nil
}

// Path returns a path inside Dir.
func Path(elem ...string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{dir}, elem...)...), nil
}

// LegacyDirs are where older dcx versions kept per-workspace files:
// hostkeys/<id>.key, locks/<id>.lock and compose/<id>/. Host keys move to
// the workspace's folder when next used; `dcx clean` removes the rest.
var LegacyDirs = struct{ HostKeys, Locks, Compose string }{"hostkeys", "locks", "compose"}
