package agentctl

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
// The installed binary is identified by a sidecar file holding its digest
// (dcxembed.Digest), so upgrading dcx replaces an older agent instead of
// keeping it forever. Reading the sidecar and installing are one
// `docker cp` each: the install writes the root-owned binary directory,
// the binary and the sidecar in one archive, so no container user can swap
// the binary the root agent executes. Unpacking replaces the binary's
// directory entry (a running agent keeps executing its old inode), and the
// sidecar comes last, so an interrupted copy is installed again next time.
// The binary is for the daemon's architecture: it is static, and the
// container shares the daemon's kernel.
//
// fresh says the container was just created or started, so it has no
// agent running; otherwise installing also signals agent processes still
// running a replaced binary (or the legacy /tmp one), and waitOutdatedGone
// then waits for them to exit. created says it was just created, so there
// is no installed agent to check.
//
// Returns true when a binary was installed.
func Deploy(ctx context.Context, engine container.Engine, containerName string, fresh, created bool) (bool, error) {
	if !created {
		if current, err := installedCurrent(ctx, engine, containerName); err != nil || current {
			return false, err
		}
	}
	info, err := engine.Info(ctx)
	if err != nil {
		return false, err
	}
	arch := info.Architecture
	if arch == "" {
		arch = runtime.GOARCH
	}
	binary, err := dcxembed.Binary(arch)
	if err != nil || len(binary) == 0 {
		return false, fmt.Errorf("no agent binary available for architecture %s: %v", arch, err)
	}
	digest, err := dcxembed.Digest(arch)
	if err != nil {
		return false, err
	}
	if err := engine.WriteFiles(ctx, containerName, []container.File{
		{Path: agentproto.StateDir, Mode: fs.ModeDir | 0o755},
		{Path: agentproto.BinaryDir, Mode: fs.ModeDir | 0o755},
		{Path: agentproto.BinaryPath, Mode: 0o755, Data: binary},
		{Path: agentproto.BinaryPath + digestSuffix, Mode: 0o644, Data: []byte(digest + "\n")},
	}); err != nil {
		return false, fmt.Errorf("failed to copy agent to container: %w", err)
	}
	if !fresh {
		if _, err := engine.ExecAsRoot(ctx, containerName, []string{
			"sh", "-c", signalOutdatedScript, "dcx-deploy", agentproto.BinaryPath, agentproto.LegacyBinaryPath,
		}); err != nil {
			return false, fmt.Errorf("failed to stop the outdated agent: %w", err)
		}
	}
	return true, nil
}

// Prefetch decompresses the agent binary for arch in the background, so a
// Deploy that follows (after creating a container) does not wait for it.
func Prefetch(arch string) {
	go func() { _, _ = dcxembed.Binary(arch) }()
}

// digestSuffix names the sidecar next to the binary.
const digestSuffix = ".sha256"

// installedCurrent reports whether the container's agent is this build's
// (for either architecture: only one can have been installed).
func installedCurrent(ctx context.Context, engine container.Engine, containerName string) (bool, error) {
	data, err := engine.ReadFile(ctx, containerName, agentproto.BinaryPath+digestSuffix)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read the installed agent's digest: %w", err)
	}
	installed := strings.TrimSpace(string(data))
	if installed == "" {
		return false, nil
	}
	for _, arch := range []string{"amd64", "arm64"} {
		if digest, err := dcxembed.Digest(arch); err == nil && digest == installed {
			return true, nil
		}
	}
	return false, nil
}

// signalOutdatedScript signals agent processes still running a replaced
// binary (/proc/<pid>/exe is "<path> (deleted)", $1) or the legacy one
// ($2). Every value is a positional argument.
const signalOutdatedScript = `for d in /proc/[0-9]*; do
  exe="$(readlink "$d/exe" 2>/dev/null)"
  case "$exe" in
    "$1 (deleted)"|"$2"|"$2 (deleted)") kill "${d#/proc/}" 2>/dev/null ;;
  esac
done
exit 0`

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
