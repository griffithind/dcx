// Package dcxhome locates the directory where dcx keeps its state on the
// host: SSH host keys, known_hosts, the fallback client key, workspace
// locks and compose overrides.
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
