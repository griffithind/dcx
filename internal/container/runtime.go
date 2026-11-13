package container

import (
	"slices"
	"strings"
)

// UpOptions configures the Up operation. Build secrets are passed here
// (the builds need them); runtime secrets are mounted by the service once
// the container runs.
type UpOptions struct {
	// Build builds images before starting containers.
	Build bool
	// Rebuild forces a rebuild of images.
	Rebuild bool
	// Pull forces re-fetch of remote resources (images, features).
	Pull bool
	// NoCache builds without BuildKit's cache (dcx build --no-cache).
	NoCache bool
	// BuildSecrets are secrets to pass to docker build (BuildKit secrets).
	// Map of secret ID to temp file path containing the secret value.
	BuildSecrets map[string]string

	// SSHAccess is who may reach the dcx-agent; recorded on the container.
	SSHAccess SSHAccess
}

// SSHAccess is who may reach a workspace's dcx-agent: loopback only (the
// zero value) or, with `dcx up --hosts`, also the listed CIDRs. Widened
// access publishes the port on every host interface and relies on the
// agent's connection allowlist as the gate.
type SSHAccess struct {
	AllowedCIDRs []string
}

// BindHost is the host interface the agent's port is published (or, with
// host networking, listened) on.
func (a SSHAccess) BindHost() string {
	if len(a.AllowedCIDRs) > 0 {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// Equal reports whether a and b allow the same CIDRs, in the same order.
func (a SSHAccess) Equal(b SSHAccess) bool {
	return slices.Equal(a.AllowedCIDRs, b.AllowedCIDRs)
}

// String describes the access for messages: "loopback" or the CIDRs.
func (a SSHAccess) String() string {
	if len(a.AllowedCIDRs) == 0 {
		return "loopback"
	}
	return strings.Join(a.AllowedCIDRs, ",")
}

// DownOptions configures the Down operation.
type DownOptions struct {
	// RemoveVolumes removes associated volumes.
	RemoveVolumes bool
	// RemoveOrphans removes containers not defined in compose file.
	RemoveOrphans bool
}

// BuildOptions configures the Build operation.
type BuildOptions struct {
	// NoCache disables build cache.
	NoCache bool
	// Pull pulls base images before building.
	Pull bool
}
