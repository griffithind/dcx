package container

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/devcontainer"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
)

// sshEndpoint is how a new container's dcx-agent is reached from the host.
// It is decided once, at create time, and recorded in the SSH labels.
type sshEndpoint struct {
	// hostPort is the host port the agent is reachable on (127.0.0.1, and
	// every interface with widened access).
	hostPort int
	// listen is the agent's listen address inside the container.
	listen string
	// publish reports whether the agent's port is published (not with host
	// networking). owner, when set, is the compose service it is published
	// on instead of the primary: the namespace owner for network_mode
	// service:<owner>.
	publish bool
	owner   string
	access  SSHAccess
}

// labels records the endpoint on the container.
func (e sshEndpoint) labels(l *state.ContainerLabels) {
	l.SSHPort = e.hostPort
	l.SSHListen = e.listen
	l.SSHAllowedCIDRs = e.access.AllowedCIDRs
}

// portForward is the publication of the agent's port.
func (e sshEndpoint) portForward() devcontainer.PortForward {
	return devcontainer.PortForward{
		ContainerPort: agentproto.ListenPort,
		HostPort:      e.hostPort,
		Host:          e.access.BindHost(),
		Protocol:      "tcp",
	}
}

// planSSH decides the agent's endpoint for a new container from its network
// mode:
//
//   - default/bridge/user networks: publish ListenPort on the container;
//   - host: nothing can be published, so the agent listens on the host port
//     directly (two host-network workspaces must not share a port);
//   - compose service:<x>: the container shares x's network namespace, and
//     Docker only publishes ports on the namespace owner, so publish there;
//   - container:<x> and none: no way to reach the agent from the host.
func (r *Provisioner) planSSH(ctx context.Context, access SSHAccess) error {
	kind, target, err := r.sshNetwork(ctx)
	if err != nil {
		return err
	}
	ep := sshEndpoint{listen: agentproto.ListenAddr("0.0.0.0"), access: access}
	switch kind {
	case "host":
	case "service":
		ep.publish, ep.owner = true, target
	default:
		ep.publish = true
	}

	ep.hostPort, err = r.chooseSSHPort(ctx)
	if err != nil {
		return err
	}
	if kind == "host" {
		ep.listen = net.JoinHostPort(access.BindHost(), strconv.Itoa(ep.hostPort))
		if len(r.resolved.ForwardPorts)+len(r.resolved.AppPorts) > 0 {
			r.report.Infof("Host networking: forwardPorts and appPort are already on the host and are not published")
		}
	}
	r.ssh = &ep
	return nil
}

// sshNetwork splits the primary container's network mode into its kind
// ("host", "service", a network name, …) and target, and rejects the modes
// that leave the agent unreachable from the host.
func (r *Provisioner) sshNetwork(ctx context.Context) (kind, target string, err error) {
	mode, err := r.networkMode(ctx)
	if err != nil {
		return "", "", err
	}
	kind, target, _ = strings.Cut(mode, ":")
	switch kind {
	case "container":
		return "", "", fmt.Errorf("network mode %q is not supported: dcx can't publish its SSH port on a container it doesn't manage; "+
			"use a compose service with network_mode: service:<name> instead", mode)
	case "none":
		return "", "", fmt.Errorf("network mode %q is not supported: dcx reaches the container over the network", mode)
	}
	return kind, target, nil
}

// networkMode returns the primary container's network mode: runArgs
// --network for single containers, the service's network_mode for compose.
func (r *Provisioner) networkMode(ctx context.Context) (string, error) {
	plan, ok := r.composePlan()
	if !ok {
		if r.resolved.RunArgs != nil {
			return r.resolved.RunArgs.NetworkMode, nil
		}
		return "", nil
	}
	svc, err := r.composeService(ctx, plan)
	if err != nil {
		return "", err
	}
	return svc.NetworkMode, nil
}

// chooseSSHPort picks the host port for the agent: the workspace's
// preferred port, or the next one that is neither bound nor recorded on
// another workspace's container. Stopped containers count: their port is
// claimed again when they start.
func (r *Provisioner) chooseSSHPort(ctx context.Context) (int, error) {
	claimed := map[int]bool{}
	rows, err := r.engine.ListContainerRows(ctx, map[string]string{state.LabelManaged: "true"}, state.LabelWorkspaceID, state.LabelSSHPort)
	if err != nil {
		return 0, fmt.Errorf("list dcx containers: %w", err)
	}
	for _, row := range rows {
		if port, err := strconv.Atoi(row.Labels[state.LabelSSHPort]); err == nil && port > 0 && row.Labels[state.LabelWorkspaceID] != r.resolved.WorkspaceID {
			claimed[port] = true
		}
	}
	port, err := dcxssh.PickPort(r.resolved.WorkspaceID, func(p int) bool {
		return !claimed[p] && dcxssh.IsHostPortAvailable(p)
	})
	if err != nil {
		return 0, err
	}
	if preferred := dcxssh.DeterministicPort(r.resolved.WorkspaceID); port != preferred {
		r.report.Infof("SSH: preferred port %d is taken; using %d", preferred, port)
	}
	return port, nil
}

// SSHHostPort returns the host port a container's agent is reachable on:
// the recorded port, or (containers created by older dcx versions) the
// port Docker published ListenPort on.
func SSHHostPort(ctx context.Context, engine Engine, info *state.ContainerInfo) (int, error) {
	if info.Labels != nil && info.Labels.SSHPort > 0 {
		return info.Labels.SSHPort, nil
	}
	return engine.PortMapping(ctx, info.Name, agentproto.ListenPort, "tcp")
}

// SSHListenAddr returns the agent's listen address inside a container.
func SSHListenAddr(info *state.ContainerInfo) string {
	if info.Labels != nil && info.Labels.SSHListen != "" {
		return info.Labels.SSHListen
	}
	return agentproto.ListenAddr("0.0.0.0")
}

// RecordedSSHAccess returns the SSH access a container was created with.
func RecordedSSHAccess(info *state.ContainerInfo) SSHAccess {
	if info == nil || info.Labels == nil {
		return SSHAccess{}
	}
	return SSHAccess{AllowedCIDRs: info.Labels.SSHAllowedCIDRs}
}

// PlannedSSHPort is the host port chosen for the agent by the last Up (0
// before one, or with host networking where nothing is published).
func (r *Provisioner) PlannedSSHPort() int {
	if r.ssh == nil || !r.ssh.publish {
		return 0
	}
	return r.ssh.hostPort
}
