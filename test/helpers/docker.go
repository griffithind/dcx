// Package helpers provides shared test utilities for dcx E2E tests.
package helpers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// ansiRegex matches ANSI escape sequences.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

var (
	dcxBinaryPath string
	dcxBinaryErr  error
	dcxBinaryOnce sync.Once
)

// RunDCX runs the dcx CLI with the given arguments.
// It returns stdout, stderr, and any error.
func RunDCX(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	// Build the dcx binary path (assumes make build was run)
	binaryPath := GetDCXBinary(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// RunDCXInDir runs dcx in a specific directory.
func RunDCXInDir(t *testing.T, dir string, args ...string) (string, string, error) {
	t.Helper()

	binaryPath := GetDCXBinary(t)
	registerWorkspace(dir)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, args...)
	cmd.Dir = dir

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return stdout.String(), stderr.String(), err
}

// RunDCXSuccess runs dcx and expects success. It returns everything dcx
// printed: stdout (data) followed by stderr (progress, warnings). Use
// RunDCX to look at one stream.
func RunDCXSuccess(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, err := RunDCX(t, args...)
	if err != nil {
		t.Fatalf("dcx %v failed: %v\nstdout: %s\nstderr: %s", args, err, stdout, stderr)
	}
	return stdout + stderr
}

// RunDCXInDirSuccess runs dcx in a directory and expects success. It
// returns everything dcx printed: stdout (data) followed by stderr
// (progress, warnings). Use RunDCXInDir to look at one stream.
func RunDCXInDirSuccess(t *testing.T, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, err := RunDCXInDir(t, dir, args...)
	if err != nil {
		t.Fatalf("dcx %v in %s failed: %v\nstdout: %s\nstderr: %s", args, dir, err, stdout, stderr)
	}
	return stdout + stderr
}

// GetDCXBinary returns bin/dcx, built by `make build`. It fails rather
// than building one itself: a plain `go build` would embed stale or
// missing agent binaries, and a binary older than the sources would test
// old code (both have produced misleading results before).
func GetDCXBinary(t *testing.T) string {
	t.Helper()

	dcxBinaryOnce.Do(func() {
		root := GetProjectRoot(t)
		binaryPath := filepath.Join(root, "bin", "dcx")
		info, err := os.Stat(binaryPath)
		if err != nil {
			dcxBinaryErr = fmt.Errorf("%s not found: run `make build` first", binaryPath)
			return
		}
		if newer := newestSource(root); newer != "" {
			if src, err := os.Stat(newer); err == nil && src.ModTime().After(info.ModTime()) {
				dcxBinaryErr = fmt.Errorf("%s is older than %s: run `make build` first", binaryPath, newer)
				return
			}
		}
		dcxBinaryPath = binaryPath
	})

	if dcxBinaryErr != nil {
		t.Fatal(dcxBinaryErr)
	}
	return dcxBinaryPath
}

// newestSource returns the most recently modified Go source that ends up
// in the dcx binary.
func newestSource(root string) string {
	var newest string
	var newestTime time.Time
	for _, dir := range []string{"cmd", "internal"} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if info, err := d.Info(); err == nil && info.ModTime().After(newestTime) {
				newest, newestTime = path, info.ModTime()
			}
			return nil
		})
	}
	return newest
}

// GetProjectRoot returns the project root directory.
func GetProjectRoot(t *testing.T) string {
	t.Helper()

	// Find go.mod to determine project root
	cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to find project root: %v", err)
	}

	return strings.TrimSpace(string(output))
}

// RequireComposeAvailable skips the test if docker compose is not available.
func RequireComposeAvailable(t *testing.T) {
	t.Helper()

	cmd := exec.Command("docker", "compose", "version")
	if err := cmd.Run(); err != nil {
		t.Skip("docker compose is not available, skipping E2E test")
	}
}

// StripANSI removes ANSI escape sequences from a string.
func StripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

// ptyTimeout bounds an interactive dcx session: a session waiting for
// input it never gets must fail the test, not hang the run.
const ptyTimeout = 2 * time.Minute

// RunDCXInPTY runs dcx in dir with a pseudo-terminal as its stdin/stdout,
// as an interactive user would, typing input. It returns the terminal
// output once dcx exits, or kills the session after ptyTimeout.
func RunDCXInPTY(t *testing.T, dir, input string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(GetDCXBinary(t), args...)
	cmd.Dir = dir
	ptmx, err := pty.Start(cmd) // a new session: dcx leads its process group
	if err != nil {
		return "", err
	}
	defer func() { _ = ptmx.Close() }()

	var timedOut atomic.Bool
	timer := time.AfterFunc(ptyTimeout, func() {
		timedOut.Store(true)
		// The whole group: OpenSSH children hold the terminal open too.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_ = ptmx.Close()
	})
	defer timer.Stop()

	go func() { _, _ = ptmx.Write([]byte(input)) }()
	var out bytes.Buffer
	_, _ = io.Copy(&out, ptmx) // ends with EIO once dcx exits
	err = cmd.Wait()
	if timer.Stop(); timedOut.Load() {
		return out.String(), fmt.Errorf("dcx %s: no exit after %s (output so far: %q)", strings.Join(args, " "), ptyTimeout, out.String())
	}
	return out.String(), err
}
