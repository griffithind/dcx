// Package agentctl provisions the dcx-agent in a container: it installs (or
// upgrades) the binary, then hands the agent everything it needs in one
// `dcx-agent provision` call. Everything goes through the container engine.
package agentctl

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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
	// container's default user, DefaultUser.
	User string
	// DefaultUser is the container's Config.User.
	DefaultUser string
	// WorkDir is where sessions start; "" means /workspace.
	WorkDir string
	// ListenAddr is the agent's listen address; "" means
	// 0.0.0.0:agentproto.ListenPort.
	ListenAddr string
	// AllowedCIDRs widens the agent's connection allowlist beyond loopback
	// (`dcx up --hosts`).
	AllowedCIDRs []string
	// NewContainer says dcx just created the container, so no agent can be
	// running in it: nothing to wait for after installing.
	NewContainer bool

	// Secrets are the runtime secrets, owned by SecretsOwner ("" is root).
	Secrets      []agentproto.Secret
	SecretsOwner string
	// Environment is written to dcx's block of /etc/environment.
	Environment map[string]string
	// Probe is the userEnvProbe ("none" or "" skips it).
	Probe string
	// Session is what every session gets; its Env (remoteEnv) is laid over
	// the probed environment.
	Session agentproto.SessionConfig
}

func (o Options) listenAddr() string {
	if o.ListenAddr == "" {
		return agentproto.ListenAddr("0.0.0.0")
	}
	return o.ListenAddr
}

// Provision makes the container's agent current, provisioned and
// listening: it installs the agent when it is missing or outdated, then
// runs `dcx-agent provision` once, as root, with the request on stdin (the
// SSH and runtime secrets, /etc/environment, the userEnvProbe, the session
// configuration, the listener). It is idempotent: a listening agent keeps
// running, and its secrets are rewritten so key changes (a regenerated
// ~/.ssh/id_ed25519) apply on the next `dcx up`.
func Provision(ctx context.Context, engine container.Engine, rep report.Reporter, opts Options) error {
	rep = report.OrDiscard(rep)
	rep.Infof("Starting dcx agent...")
	installed, err := Deploy(ctx, engine, opts.ContainerName)
	if err != nil {
		return fmt.Errorf("failed to install dcx agent: %w", err)
	}
	if installed && !opts.NewContainer {
		if err := waitOutdatedGone(ctx, engine, opts.ContainerName, opts.listenAddr()); err != nil {
			return err
		}
	}

	req, err := request(opts)
	if err != nil {
		return err
	}
	input, err := json.Marshal(req)
	if err != nil {
		return err
	}
	out, runErr := engine.ExecAsRootWithInput(ctx, opts.ContainerName, []string{agentproto.BinaryPath, "provision"}, input)
	var res agentproto.ProvisionResult
	if err := json.Unmarshal(out, &res); err != nil {
		if runErr != nil {
			return fmt.Errorf("failed to provision dcx agent: %w", runErr)
		}
		return fmt.Errorf("failed to provision dcx agent: unreadable result: %w", err)
	}
	for _, w := range res.Warnings {
		rep.Warnf("%s", w)
	}
	if len(res.Skipped) > 0 {
		rep.Warnf("Not written to /etc/environment (name or value it cannot express; sessions still get them): %s", strings.Join(res.Skipped, ", "))
	}
	if res.Error != "" {
		return fmt.Errorf("failed to provision dcx agent: %s", res.Error)
	}
	if runErr != nil {
		return fmt.Errorf("failed to provision dcx agent: %w", runErr)
	}
	return nil
}

// request builds the provision request: the workspace's persistent host
// key and the user's authorized public keys, then opts.
func request(opts Options) (agentproto.ProvisionRequest, error) {
	keyPath, _, err := dcxssh.EnsureHostKey(opts.WorkspaceID)
	if err != nil {
		return agentproto.ProvisionRequest{}, fmt.Errorf("ensure host key: %w", err)
	}
	hostKey, err := os.ReadFile(keyPath)
	if err != nil {
		return agentproto.ProvisionRequest{}, fmt.Errorf("read host key: %w", err)
	}
	authKeys, err := dcxssh.AuthorizedKeys()
	if err != nil {
		return agentproto.ProvisionRequest{}, fmt.Errorf("collect authorized keys: %w", err)
	}
	return agentproto.ProvisionRequest{
		HostKey:        hostKey,
		AuthorizedKeys: authKeys,
		Secrets:        opts.Secrets,
		SecretsOwner:   opts.SecretsOwner,
		Environment:    opts.Environment,
		Probe:          opts.Probe,
		ProbeUser:      opts.User,
		Session:        opts.Session,
		Listen: agentproto.ListenConfig{
			Addr:       opts.listenAddr(),
			User:       opts.User,
			WorkDir:    opts.WorkDir,
			AllowCIDRs: opts.AllowedCIDRs,
		},
		DefaultUser: opts.DefaultUser,
	}, nil
}

// Listening reports whether the container's agent, listening on
// listenAddr, answers a ping (on loopback, from inside the container).
func Listening(ctx context.Context, engine container.Engine, containerName, listenAddr string) bool {
	return engine.ExecInContainer(ctx, containerName, []string{
		agentproto.BinaryPath, "ping", "--addr", agentproto.PingAddr(listenAddr),
	}) == nil
}
