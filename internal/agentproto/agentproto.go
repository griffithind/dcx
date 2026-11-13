// Package agentproto holds the contract shared by host-side dcx and the
// in-container dcx-agent: where the agent lives, where it listens and where
// dcx-managed secrets are mounted. Both sides import these values so they
// cannot drift apart. It must stay dependency-free (stdlib only) because it
// is compiled into the agent binary.
package agentproto

import "strconv"

const (
	// BinaryPath is where dcx-agent is deployed inside containers.
	BinaryPath = "/tmp/dcx-agent"

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

	// ReadyFile is written by the agent once it accepts connections.
	ReadyFile = "/var/lib/dcx/agent-ready"
)

// ListenAddr returns "<host>:<ListenPort>".
func ListenAddr(host string) string {
	return host + ":" + strconv.Itoa(ListenPort)
}

// LoopbackAddr is the address used to probe the agent from inside the container.
var LoopbackAddr = ListenAddr("127.0.0.1")
