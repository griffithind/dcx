// Package deploy provides dcx-agent binary deployment to containers.
package deploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	dcxembed "github.com/griffithind/dcx"
	"github.com/griffithind/dcx/internal/agentproto"
)

// DeployToContainer makes sure the container has the agent binary that
// matches this dcx build.
//
// The installed binary is identified by a sidecar file holding its SHA-256,
// so upgrading dcx replaces an older agent instead of keeping it forever.
// The new binary is copied to a temporary path and renamed into place, so an
// interrupted copy never leaves a truncated binary at binaryPath (a running
// agent keeps executing its old inode).
//
// Returns true when a binary was installed; an older agent process may then
// still be running and should be stopped with StopOutdatedAgent.
func DeployToContainer(ctx context.Context, containerName, binaryPath string) (bool, error) {
	containerArch := getContainerArch(ctx, containerName)
	binary, err := dcxembed.GetBinary(containerArch)
	if err != nil || len(binary) == 0 {
		return false, fmt.Errorf("no agent binary available for architecture %s: %v", containerArch, err)
	}
	want := sha256Hex(binary)

	if installedDigest(ctx, containerName, binaryPath) == want {
		return false, nil
	}

	tmpFile, err := os.CreateTemp("", "dcx-agent-*")
	if err != nil {
		return false, fmt.Errorf("create temp agent binary: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()
	if _, err := tmpFile.Write(binary); err != nil {
		_ = tmpFile.Close()
		return false, fmt.Errorf("write temp agent binary: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return false, fmt.Errorf("write temp agent binary: %w", err)
	}

	staging := binaryPath + ".new"
	if out, err := exec.CommandContext(ctx, "docker", "cp", tmpFile.Name(), containerName+":"+staging).CombinedOutput(); err != nil {
		return false, fmt.Errorf("failed to copy agent to container: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// chmod, atomic rename, then record the digest. Paths are positional
	// arguments, never interpolated into the script.
	script := `chmod 0755 "$1" && mv -f "$1" "$2" && printf '%s\n' "$3" > "$2.sha256"`
	install := exec.CommandContext(ctx, "docker", "exec", "--user", "root", containerName,
		"sh", "-c", script, "dcx-deploy", staging, binaryPath, want)
	if out, err := install.CombinedOutput(); err != nil {
		return false, fmt.Errorf("failed to install agent: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// installedDigest returns the recorded digest of the installed agent, or ""
// when the binary or its digest file is missing.
func installedDigest(ctx context.Context, containerName, binaryPath string) string {
	script := `test -x "$1" && cat "$1.sha256"`
	out, err := exec.CommandContext(ctx, "docker", "exec", containerName,
		"sh", "-c", script, "dcx-deploy", binaryPath).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// StopOutdatedAgent terminates agent processes still running a binary that
// has been replaced on disk (their /proc/<pid>/exe is "<path> (deleted)"),
// then waits for the listener port to be released so a fresh agent can bind.
func StopOutdatedAgent(ctx context.Context, containerName, binaryPath string, listenAddr string) error {
	script := `for d in /proc/[0-9]*; do
  if [ "$(readlink "$d/exe" 2>/dev/null)" = "$1 (deleted)" ]; then kill "${d#/proc/}" 2>/dev/null; fi
done
exit 0`
	kill := exec.CommandContext(ctx, "docker", "exec", "--user", "root", containerName,
		"sh", "-c", script, "dcx-deploy", binaryPath)
	if out, err := kill.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop outdated agent: %w: %s", err, strings.TrimSpace(string(out)))
	}

	// The agent drains sessions for up to 10s on SIGTERM.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ping := exec.CommandContext(ctx, "docker", "exec", containerName, binaryPath, "ping", "--addr", listenAddr)
		if err := ping.Run(); err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("outdated agent still listening on %s", listenAddr)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func getContainerArch(ctx context.Context, containerName string) string {
	cmd := exec.CommandContext(ctx, "docker", "exec", containerName, "uname", "-m")
	output, err := cmd.Output()
	if err != nil {
		return runtime.GOARCH
	}
	return strings.TrimSpace(string(bytes.TrimSpace(output)))
}

// GetContainerBinaryPath returns the path for dcx-agent binary in the container.
func GetContainerBinaryPath() string {
	return agentproto.BinaryPath
}

// PreDeployAgent deploys the dcx-agent binary to the specified container.
// Returns true when a new binary was installed.
func PreDeployAgent(ctx context.Context, containerName string) (bool, error) {
	return DeployToContainer(ctx, containerName, GetContainerBinaryPath())
}
