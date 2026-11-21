package agentctl

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
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
// Installing also signals agent processes still running a replaced binary
// (or the legacy /tmp one); waitOutdatedGone then waits for them to exit.
// The probe, the copy and the install are three execs.
//
// Returns true when a binary was installed.
func Deploy(ctx context.Context, engine container.Engine, containerName string) (bool, error) {
	return deployTo(ctx, engine, containerName, agentproto.BinaryPath)
}

func deployTo(ctx context.Context, engine container.Engine, containerName, binaryPath string) (bool, error) {
	arch, installed, err := probe(ctx, engine, containerName, binaryPath)
	if err != nil {
		return false, err
	}
	binary, err := dcxembed.Binary(arch)
	if err != nil || len(binary) == 0 {
		return false, fmt.Errorf("no agent binary available for architecture %s: %v", arch, err)
	}
	want := sha256Hex(binary)
	if installed == want {
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
	if err := engine.CopyToContainer(ctx, tmpFile.Name(), containerName, staging); err != nil {
		return false, fmt.Errorf("failed to copy agent to container: %w", err)
	}

	if _, err := engine.ExecAsRoot(ctx, containerName, []string{
		"sh", "-c", installScript, "dcx-deploy", staging, binaryPath, want, agentproto.LegacyBinaryPath,
	}); err != nil {
		return false, fmt.Errorf("failed to install agent: %w", err)
	}
	return true, nil
}

// probeScript prints the container's architecture and the installed
// agent's recorded digest (nothing when the binary or digest is missing),
// after making sure the binary's directory ($1's) exists, root-owned:
// install -d fixes owner and mode even when it exists.
const probeScript = `uname -m
install -d -o root -g root -m 0755 "$(dirname "$1")"
if [ -x "$1" ]; then cat "$1.sha256" 2>/dev/null; fi
exit 0`

// installScript moves the staged binary ($1) into place ($2) atomically,
// records its digest ($3), then signals agent processes still running a
// replaced binary (/proc/<pid>/exe is "<path> (deleted)") or the legacy
// one ($4). Every value is a positional argument.
const installScript = `chown root:root "$1" && chmod 0755 "$1" && mv -f "$1" "$2" && printf '%s\n' "$3" > "$2.sha256" || exit 1
for d in /proc/[0-9]*; do
  exe="$(readlink "$d/exe" 2>/dev/null)"
  case "$exe" in
    "$2 (deleted)"|"$4"|"$4 (deleted)") kill "${d#/proc/}" 2>/dev/null ;;
  esac
done
exit 0`

// probe returns the container's architecture and the installed agent's
// digest ("" when there is none).
func probe(ctx context.Context, engine container.Engine, containerName, binaryPath string) (arch, digest string, err error) {
	out, err := engine.ExecAsRoot(ctx, containerName, []string{"sh", "-c", probeScript, "dcx-deploy", binaryPath})
	if err != nil {
		return "", "", fmt.Errorf("failed to prepare the agent directory: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	arch = strings.TrimSpace(lines[0])
	if arch == "" {
		arch = runtime.GOARCH
	}
	if len(lines) > 1 {
		digest = strings.TrimSpace(lines[len(lines)-1])
	}
	return arch, digest, nil
}

// waitOutdatedGone waits until no agent answers on listenAddr, after
// Deploy signalled outdated ones, so a fresh agent can bind.
func waitOutdatedGone(ctx context.Context, engine container.Engine, containerName, listenAddr string) error {
	return waitGone(ctx, engine, containerName, agentproto.BinaryPath, agentproto.PingAddr(listenAddr), 15*time.Second)
}

func waitGone(ctx context.Context, engine container.Engine, containerName, binaryPath, listenAddr string, timeout time.Duration) error {
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
