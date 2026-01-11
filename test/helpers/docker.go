// Package helpers provides shared test utilities for dcx E2E tests.
package helpers

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/require"
)

// ansiRegex matches ANSI escape sequences.
var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// TestPrefix is used to identify test containers for cleanup.
const TestPrefix = "dcx-e2e-test-"

var (
	dcxBinaryPath string
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

// RunDCXSuccess runs dcx and expects success.
func RunDCXSuccess(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, err := RunDCX(t, args...)
	if err != nil {
		t.Fatalf("dcx %v failed: %v\nstdout: %s\nstderr: %s", args, err, stdout, stderr)
	}
	return stdout
}

// RunDCXInDirSuccess runs dcx in a directory and expects success.
func RunDCXInDirSuccess(t *testing.T, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, err := RunDCXInDir(t, dir, args...)
	if err != nil {
		t.Fatalf("dcx %v in %s failed: %v\nstdout: %s\nstderr: %s", args, dir, err, stdout, stderr)
	}
	return stdout
}

// GetDCXBinary returns the path to the dcx binary.
func GetDCXBinary(t *testing.T) string {
	t.Helper()

	dcxBinaryOnce.Do(func() {
		root := GetProjectRoot(t)
		binaryPath := filepath.Join(root, "bin", "dcx")

		// Check if binary exists
		if _, err := os.Stat(binaryPath); err == nil {
			// Verify it works
			cmd := exec.Command(binaryPath, "--version")
			if err := cmd.Run(); err == nil {
				dcxBinaryPath = binaryPath
				return
			}
		}

		// Binary doesn't exist or doesn't work, build it
		t.Logf("Building dcx binary...")
		buildCmd := exec.Command("go", "build", "-o", binaryPath, "./cmd/dcx")
		buildCmd.Dir = root
		output, err := buildCmd.CombinedOutput()
		if err != nil {
			t.Fatalf("failed to build dcx: %v\noutput: %s", err, output)
		}

		dcxBinaryPath = binaryPath
	})

	if dcxBinaryPath == "" {
		t.Fatal("dcx binary path not set")
	}

	return dcxBinaryPath
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

// DockerClient returns a Docker client for test operations.
func DockerClient(t *testing.T) *container.DockerClient {
	t.Helper()

	client, err := container.NewDockerClient()
	require.NoError(t, err, "failed to create Docker client")

	t.Cleanup(func() {
		_ = client.Close()
	})

	return client
}

// CleanupTestContainers removes all test containers with the given workspace ID prefix.
func CleanupTestContainers(t *testing.T, workspaceID string) {
	t.Helper()

	ctx := context.Background()
	client := DockerClient(t)

	// Find containers with our label
	containers, err := client.ListContainersWithLabels(ctx, map[string]string{
		state.LabelWorkspaceID: workspaceID,
	})
	if err != nil {
		t.Logf("Warning: failed to list containers for cleanup: %v", err)
		return
	}

	for _, c := range containers {
		// Stop if running
		if c.Running {
			if err := client.StopContainer(ctx, c.ID, nil); err != nil {
				t.Logf("Warning: failed to stop container %s: %v", c.ID[:12], err)
			}
		}

		// Remove container and volumes
		if err := client.RemoveContainer(ctx, c.ID, true, true); err != nil {
			t.Logf("Warning: failed to remove container %s: %v", c.ID[:12], err)
		}
	}
}

// WaitForState waits for the devcontainer to reach a specific state.
func WaitForState(t *testing.T, dir string, expectedState string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		stdout := RunDCXInDirSuccess(t, dir, "status")

		if strings.Contains(stdout, fmt.Sprintf("State:      %s", expectedState)) {
			return
		}

		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("timeout waiting for state %s", expectedState)
}

// ContainerIsRunning checks if a container with the given workspace ID is running.
func ContainerIsRunning(t *testing.T, workspaceID string) bool {
	t.Helper()

	ctx := context.Background()
	client := DockerClient(t)

	containers, err := client.ListContainersWithLabels(ctx, map[string]string{
		state.LabelWorkspaceID: workspaceID,
		state.LabelIsPrimary:   "true",
	})
	require.NoError(t, err)

	if len(containers) == 0 {
		return false
	}

	return containers[0].Running
}

// GetContainerState returns the current state string from dcx status.
func GetContainerState(t *testing.T, dir string) string {
	t.Helper()

	stdout := RunDCXInDirSuccess(t, dir, "status")

	// Parse state from output (strip ANSI color codes first)
	for _, line := range strings.Split(stdout, "\n") {
		cleanLine := ansiRegex.ReplaceAllString(line, "")
		if strings.HasPrefix(cleanLine, "State:") {
			parts := strings.Fields(cleanLine)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}

	return ""
}

// RequireDockerAvailable skips the test if Docker is not available.
func RequireDockerAvailable(t *testing.T) {
	t.Helper()

	cmd := exec.Command("docker", "info")
	if err := cmd.Run(); err != nil {
		t.Skip("Docker is not available, skipping E2E test")
	}
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

// ContainsLabel checks if the output contains a label:value pair.
// It strips ANSI codes and checks for the pattern "Label: value" with flexible spacing.
func ContainsLabel(output, label, value string) bool {
	cleaned := StripANSI(output)
	// Check for label followed by value on same line (with any spacing)
	for _, line := range strings.Split(cleaned, "\n") {
		if strings.Contains(line, label+":") && strings.Contains(line, value) {
			return true
		}
	}
	return false
}
