package container

// UpOptions configures the Up operation.
//
// Note: Runtime secrets (mounted at /run/secrets) are handled by the service
// layer after Up() returns, because the service layer has access to the
// container name via stateManager. Build secrets are passed here because
// they're needed during the docker build phase.
type UpOptions struct {
	// Build builds images before starting containers.
	Build bool
	// Rebuild forces a rebuild of images.
	Rebuild bool
	// Pull forces re-fetch of remote resources (images, features).
	Pull bool
	// BuildSecrets are secrets to pass to docker build (BuildKit secrets).
	// Map of secret ID to temp file path containing the secret value.
	BuildSecrets map[string]string

	// SSHBindHost is the host interface the dcx-agent SSH port is published
	// on. Empty means 127.0.0.1 (loopback-only); "0.0.0.0" exposes it on
	// every host interface, gated by the agent's ConnCallback.
	SSHBindHost string
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
