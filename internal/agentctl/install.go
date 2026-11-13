package agentctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	dcxembed "github.com/griffithind/dcx"
	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container"
)

// Deploy makes sure the container has the agent binary that matches this
// dcx build, at agentproto.BinaryPath.
//
// The installed binary is identified by a sidecar file holding its SHA-256,
// so upgrading dcx replaces an older agent instead of keeping it forever.
// The new binary is copied to a staging path and renamed into place, so an
// interrupted copy never leaves a truncated binary (a running agent keeps
// executing its old inode). The binary directory is root-owned, so no
// container user can swap the binary the root agent executes.
//
// Returns true when a binary was installed; an older agent process may then
// still be running and should be stopped with StopOutdated.
func Deploy(ctx context.Context, engine container.Engine, containerName string) (bool, error) {
	return deployTo(ctx, engine, containerName, agentproto.BinaryPath)
}

func deployTo(ctx context.Context, engine container.Engine, containerName, binaryPath string) (bool, error) {
	arch := containerArch(ctx, engine, containerName)
	binary, err := dcxembed.GetBinary(arch)
	if err != nil || len(binary) == 0 {
		return false, fmt.Errorf("no agent binary available for architecture %s: %v", arch, err)
	}
	want := sha256Hex(binary)
	if installedDigest(ctx, engine, containerName, binaryPath) == want {
		return false, nil
	}

	// install -d fixes ownership and mode even when the directory exists.
	if _, err := engine.ExecAsRoot(ctx, containerName, []string{
		"install", "-d", "-o", "root", "-g", "root", "-m", "0755", filepath.Dir(binaryPath),
	}); err != nil {
		return false, fmt.Errorf("failed to create agent directory: %w", err)
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
	if err := engine.CopyToContainer(ctx, tmpFile.Name(), containerName, staging); err != nil {
		return false, fmt.Errorf("failed to copy agent to container: %w", err)
	}

	// chown, chmod, atomic rename, then record the digest. Paths are
	// positional arguments, never interpolated into the script.
	const script = `chown root:root "$1" && chmod 0755 "$1" && mv -f "$1" "$2" && printf '%s\n' "$3" > "$2.sha256"`
	if _, err := engine.ExecAsRoot(ctx, containerName, []string{
		"sh", "-c", script, "dcx-deploy", staging, binaryPath, want,
	}); err != nil {
		return false, fmt.Errorf("failed to install agent: %w", err)
	}
	return true, nil
}

// installedDigest returns the recorded digest of the installed agent, or ""
// when the binary or its digest file is missing.
func installedDigest(ctx context.Context, engine container.Engine, containerName, binaryPath string) string {
	out, err := engine.ExecOutput(ctx, containerName, []string{
		"sh", "-c", `test -x "$1" && cat "$1.sha256"`, "dcx-deploy", binaryPath,
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// StopOutdated terminates agent processes still running a binary that has
// been replaced on disk (their /proc/<pid>/exe is "<path> (deleted)") or
// running from the legacy /tmp location, then waits for the listener port
// to be released so a fresh agent can bind.
func StopOutdated(ctx context.Context, engine container.Engine, containerName string) error {
	return stopOutdated(ctx, engine, containerName, agentproto.BinaryPath, agentproto.LoopbackAddr, 15*time.Second)
}

func stopOutdated(ctx context.Context, engine container.Engine, containerName, binaryPath, listenAddr string, timeout time.Duration) error {
	const script = `for d in /proc/[0-9]*; do
  exe="$(readlink "$d/exe" 2>/dev/null)"
  case "$exe" in
    "$1 (deleted)"|"$2"|"$2 (deleted)") kill "${d#/proc/}" 2>/dev/null ;;
  esac
done
exit 0`
	if _, err := engine.ExecAsRoot(ctx, containerName, []string{
		"sh", "-c", script, "dcx-deploy", binaryPath, agentproto.LegacyBinaryPath,
	}); err != nil {
		return fmt.Errorf("failed to stop outdated agent: %w", err)
	}

	// The agent drains sessions for up to 10s on SIGTERM.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if engine.ExecInContainer(ctx, containerName, []string{binaryPath, "ping", "--addr", listenAddr}) != nil {
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

// containerArch returns the container's architecture as reported by
// `uname -m`, falling back to the host's.
func containerArch(ctx context.Context, engine container.Engine, containerName string) string {
	out, err := engine.ExecOutput(ctx, containerName, []string{"uname", "-m"})
	if err != nil {
		return runtime.GOARCH
	}
	return strings.TrimSpace(string(out))
}
