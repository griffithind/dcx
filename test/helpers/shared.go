package helpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Shared is a devcontainer brought up once for the whole test run and used
// by every test that asks for its key: most checks only read a running
// environment, and a container per check made the suite slow.
//
// Tests using a Shared environment must not change it (no stop, down,
// restart, --recreate, config edits) and must tolerate other tests'
// sessions running in it at the same time.
type Shared struct {
	// Dir is the workspace.
	Dir string
	// UpOutput is what `dcx up` printed (stdout, then stderr).
	UpOutput string
	// Name replaced "{{name}}" in the files: the devcontainer's name.
	Name string

	once sync.Once
	err  error
}

var (
	sharedMu  sync.Mutex
	sharedEnv = map[string]*Shared{}
)

// SharedWorkspace returns the shared environment key, bringing it up with
// files (path relative to the workspace -> content; "{{name}}" in them is
// replaced by a unique name) the first time any test asks for it. Later
// callers wait for that `up` and fail the same way if it failed.
func SharedWorkspace(t *testing.T, key string, files map[string]string) *Shared {
	t.Helper()
	RequireDockerAvailable(t)
	s := sharedEntry(key)
	bin := GetDCXBinary(t)
	s.once.Do(func() { s.err = s.bringUp(bin, key, files) })
	if s.err != nil {
		t.Fatalf("shared environment %q: %v", key, s.err)
	}
	return s
}

// StartShared begins bringing the shared environment key up in the
// background (a no-op once it has started), so tests that later ask for it
// with SharedWorkspace wait less. Tests waiting for an environment hold a
// parallel slot, so starting them all at once matters.
func StartShared(t *testing.T, key string, files map[string]string) {
	t.Helper()
	s := sharedEntry(key)
	bin := GetDCXBinary(t)
	go s.once.Do(func() { s.err = s.bringUp(bin, key, files) })
}

func sharedEntry(key string) *Shared {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	s, ok := sharedEnv[key]
	if !ok {
		s = &Shared{}
		sharedEnv[key] = s
	}
	return s
}

func (s *Shared) bringUp(bin, key string, files map[string]string) error {
	dir, err := os.MkdirTemp("", "dcx-shared-"+key+"-")
	if err != nil {
		return err
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	s.Dir = dir
	registerWorkspace(dir)
	name := fmt.Sprintf("shared_%s_%s", key, randomSuffix(6))
	s.Name = name
	for rel, content := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		content = strings.ReplaceAll(content, "{{name}}", name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			return err
		}
	}
	cmd := exec.Command(bin, "up")
	cmd.Dir = dir
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	s.UpOutput = stdout.String() + stderr.String()
	if err != nil {
		return fmt.Errorf("dcx up: %w\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	return nil
}

// DownSharedWorkspaces takes every shared environment down and removes its
// workspace. TestMain calls it after m.Run.
func DownSharedWorkspaces() {
	sharedMu.Lock()
	defer sharedMu.Unlock()
	for _, s := range sharedEnv {
		if s.Dir == "" {
			continue
		}
		if dcxBinaryPath != "" {
			cmd := exec.Command(dcxBinaryPath, "down")
			cmd.Dir = s.Dir
			_ = cmd.Run()
		}
		_ = os.RemoveAll(s.Dir)
	}
}
