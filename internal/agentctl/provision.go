// Package agentctl provisions the dcx-agent in a container: it installs (or
// upgrades) the binary, writes the agent's SSH secrets, prepares its
// root-owned directories and makes sure it is listening. Everything goes
// through the container engine.
package agentctl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/report"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
)

// Options describes the agent to provision.
type Options struct {
	ContainerName string
	// WorkspaceID selects the workspace's persistent SSH host key.
	WorkspaceID string
	// User is the user sessions run as (remoteUser); "" means the
	// container's default user.
	User string
	// WorkDir is where sessions start; "" means /workspace.
	WorkDir string
	// ListenAddr is the agent's listen address; "" means
	// 0.0.0.0:agentproto.ListenPort.
	ListenAddr string
	// AllowedCIDRs widens the agent's connection allowlist beyond loopback
	// (`dcx up --hosts`).
	AllowedCIDRs []string
}

func (o Options) listenAddr() string {
	if o.ListenAddr == "" {
		return agentproto.ListenAddr("0.0.0.0")
	}
	return o.ListenAddr
}

// readyTimeout bounds how long Provision waits for a launched agent.
var readyTimeout = 15 * time.Second

// defaultUIDQuery prints the container default user's uid.
var defaultUIDQuery = []string{"id", "-u"}

// Provision makes the container's agent current and listening. It is
// idempotent: an up-to-date, listening agent is left alone apart from its
// secrets, which are rewritten so host key changes (a regenerated
// ~/.ssh/id_ed25519) apply on the next `dcx up`.
func Provision(ctx context.Context, engine container.Engine, rep report.Reporter, opts Options) error {
	rep = report.OrDiscard(rep)
	rep.Infof("Installing dcx agent...")
	installed, err := Deploy(ctx, engine, opts.ContainerName)
	if err != nil {
		return fmt.Errorf("failed to install dcx agent: %w", err)
	}
	if installed {
		if err := StopOutdated(ctx, engine, opts.ContainerName, opts.listenAddr()); err != nil {
			return err
		}
	}
	if err := writeSecrets(ctx, engine, opts); err != nil {
		return fmt.Errorf("failed to mount SSH secrets: %w", err)
	}
	if err := launch(ctx, engine, opts); err != nil {
		return fmt.Errorf("failed to launch SSH agent: %w", err)
	}
	return nil
}

// writeSecrets writes the workspace's persistent host key and the user's
// authorized public keys into /run/secrets/dcx/.
//
// Both are root-owned, like the agent that reads them; the host key is
// 0400.
func writeSecrets(ctx context.Context, engine container.Engine, opts Options) error {
	keyPath, _, err := dcxssh.EnsureHostKey(opts.WorkspaceID)
	if err != nil {
		return fmt.Errorf("ensure host key: %w", err)
	}
	hostKey, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("read host key: %w", err)
	}
	authKeys, err := dcxssh.AuthorizedKeys()
	if err != nil {
		return fmt.Errorf("collect authorized keys: %w", err)
	}
	return container.MountDCXSecrets(ctx, engine, opts.ContainerName, []container.DCXSecret{
		{Name: "authorized_keys", Value: authKeys, Mode: "0444", Owner: "root"},
		{Name: "ssh_host_ed25519_key", Value: hostKey, Mode: "0400", Owner: "root"},
	})
}

// launch starts the agent's SSH listener unless one already answers, then
// waits until it does, so lifecycle hooks (which connect over SSH) don't
// race its startup.
func launch(ctx context.Context, engine container.Engine, opts Options) error {
	addr := opts.listenAddr()
	if Listening(ctx, engine, opts.ContainerName, addr) {
		return nil
	}

	// The agent runs as root and switches to the session user for each
	// session, so any remoteUser works whatever the image's USER is.
	// Without one, sessions run as the container's default user.
	user := opts.User
	if user == "" {
		uid, err := engine.ExecOutput(ctx, opts.ContainerName, defaultUIDQuery)
		if err != nil {
			return fmt.Errorf("determine the container's default user: %w", err)
		}
		user = strings.TrimSpace(string(uid))
	}
	workDir := opts.WorkDir
	if workDir == "" {
		workDir = "/workspace"
	}
	argv := []string{
		agentproto.BinaryPath, "listen",
		"--addr", addr,
		"--host-key", agentproto.HostKeyPath,
		"--authorized-keys", agentproto.AuthorizedKeysPath,
		"--user", user,
		"--workdir", workDir,
	}
	if len(opts.AllowedCIDRs) > 0 {
		argv = append(argv, "--allow-cidrs", strings.Join(opts.AllowedCIDRs, ","))
	}

	// `dcx-agent setup` prepares the root-owned directories, shell
	// integration and log file.
	if _, err := engine.ExecAsRoot(ctx, opts.ContainerName, []string{
		agentproto.BinaryPath, "setup",
	}); err != nil {
		return fmt.Errorf("prepare agent: %w", err)
	}

	// Keep the agent's output in its log so a failed start can be explained
	// (`docker exec -d` discards it). argv is passed as positional
	// arguments, never interpolated into the script.
	wrapped := append([]string{"sh", "-c", `exec "$0" "$@" >` + agentproto.LogPath + ` 2>&1`}, argv...)
	if err := engine.ExecDetachedAsRoot(ctx, opts.ContainerName, wrapped); err != nil {
		return err
	}

	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		if Listening(ctx, engine, opts.ContainerName, addr) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	msg := fmt.Sprintf("dcx agent did not start listening within %s", readyTimeout)
	if out, err := engine.ExecOutput(ctx, opts.ContainerName, []string{"tail", "-n", "20", agentproto.LogPath}); err == nil {
		if log := strings.TrimSpace(string(out)); log != "" {
			msg += "; agent log:\n" + log
		}
	}
	return errors.New(msg)
}

// Listening reports whether the container's agent, listening on
// listenAddr, answers a ping (on loopback, from inside the container).
func Listening(ctx context.Context, engine container.Engine, containerName, listenAddr string) bool {
	return engine.ExecInContainer(ctx, containerName, []string{
		agentproto.BinaryPath, "ping", "--addr", agentproto.PingAddr(listenAddr),
	}) == nil
}
