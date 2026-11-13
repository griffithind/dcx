// Package agentproto holds the contract shared by host-side dcx and the
// in-container dcx-agent: where the agent lives, where it listens, where
// dcx-managed secrets are mounted and what per-session configuration dcx
// hands the agent. Both sides import these values so they cannot drift
// apart. It must stay dependency-free (stdlib only) because it is compiled
// into the agent binary.
//
// Everything the agent (running as root) reads or writes lives in
// root-owned directories (/var/lib/dcx, /run/dcx), never in world-writable
// /tmp where a container user could plant symlinks or replace files.
package agentproto

import "strconv"

const (
	// StateDir holds persistent dcx state inside the container: the agent
	// binary, its log, readiness sentinel and lifecycle markers.
	StateDir = "/var/lib/dcx"

	// BinaryDir is the root-owned directory the agent binary is installed in.
	BinaryDir = StateDir + "/bin"

	// BinaryPath is where dcx-agent is deployed inside containers.
	BinaryPath = BinaryDir + "/dcx-agent"

	// LegacyBinaryPath is where older dcx versions deployed the agent.
	// Agents still running from it are stopped on upgrade.
	LegacyBinaryPath = "/tmp/dcx-agent"

	// LogPath receives the agent's stdout/stderr.
	LogPath = StateDir + "/agent.log"

	// ReadyFile is written by the agent once it accepts connections.
	ReadyFile = StateDir + "/agent-ready"

	// RunDir holds runtime files the agent reads per session. It is
	// rewritten on every start.
	RunDir = "/run/dcx"

	// ShellIntegrationDir holds the shell integration scripts sessions
	// source (terminal titles).
	ShellIntegrationDir = RunDir + "/shell"

	// SessionConfigPath is the JSON-encoded SessionConfig dcx writes on each
	// `dcx up`; the agent reads it for every session.
	SessionConfigPath = RunDir + "/session.json"

	// ListenPort is the container-side TCP port the agent's SSH server binds.
	ListenPort = 48022

	// SecretsDir is the tmpfs where runtime secrets are mounted.
	SecretsDir = "/run/secrets"

	// DCXSecretsDir holds dcx-managed secrets (host key, authorized_keys),
	// namespaced so they cannot collide with user secret names.
	DCXSecretsDir = SecretsDir + "/dcx"

	// HostKeyPath is the persistent SSH host private key.
	HostKeyPath = DCXSecretsDir + "/ssh_host_ed25519_key"

	// AuthorizedKeysPath lists the public keys allowed to connect.
	AuthorizedKeysPath = DCXSecretsDir + "/authorized_keys"
)

// SessionConfig is what every SSH session gets, whether it comes from
// `dcx shell`, plain `ssh`, an editor or scp: the devcontainer's remote
// environment and its postAttachCommand.
type SessionConfig struct {
	// Env is applied to every session: the userEnvProbe result overlaid
	// with remoteEnv (${containerEnv:…} already resolved).
	Env map[string]string `json:"env,omitempty"`

	// PostAttach runs at the start of each interactive session (a PTY
	// without a command), before the shell starts.
	PostAttach []AttachStep `json:"postAttach,omitempty"`
}

// AttachStep is one postAttachCommand source (a feature or
// devcontainer.json). Steps run in order and stop at the first failure;
// the commands of one step run in parallel (the object form of a
// lifecycle command).
type AttachStep struct {
	Label    string          `json:"label"`
	Commands []AttachCommand `json:"commands"`
}

// AttachCommand is a single command: a shell string run with /bin/sh -c,
// or an argv executed directly.
type AttachCommand struct {
	Name  string   `json:"name,omitempty"`
	Shell string   `json:"shell,omitempty"`
	Argv  []string `json:"argv,omitempty"`
}

// ListenAddr returns "<host>:<ListenPort>".
func ListenAddr(host string) string {
	return host + ":" + strconv.Itoa(ListenPort)
}

// LoopbackAddr is the address used to probe the agent from inside the container.
var LoopbackAddr = ListenAddr("127.0.0.1")
