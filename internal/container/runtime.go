package container

import (
	"context"
	"io"
)

// ContainerRuntime represents a devcontainer environment that can be started, stopped, and managed.
// This interface abstracts the differences between compose and single-container runners.
//
// This replaces the previous runner.Environment interface with a clearer name.
type ContainerRuntime interface {
	// Up starts the environment, building images if necessary.
	Up(ctx context.Context, opts UpOptions) error

	// Start starts an existing stopped environment.
	Start(ctx context.Context) error

	// Stop stops a running environment.
	Stop(ctx context.Context) error

	// Down removes the environment and optionally its resources.
	Down(ctx context.Context, opts DownOptions) error

	// Build builds the environment images without starting containers.
	Build(ctx context.Context, opts BuildOptions) error
}

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

// ExecOptions configures the Exec operation.
type ExecOptions struct {
	// WorkingDir sets the working directory for the command.
	WorkingDir string
	// User sets the user to run the command as.
	User string
	// Env sets additional environment variables.
	Env []string
	// Stdin provides input to the command.
	Stdin io.Reader
	// Stdout receives command output.
	Stdout io.Writer
	// Stderr receives command error output.
	Stderr io.Writer
	// TTY allocates a pseudo-TTY.
	TTY bool
	// SSHAgentEnabled enables SSH agent forwarding.
	SSHAgentEnabled bool
}

// ContainerInfo contains information about a running container.
type ContainerInfo struct {
	ID        string            // Container ID
	Name      string            // Container name
	Image     string            // Image used
	Status    string            // Container status
	Running   bool              // Is container running
	Labels    map[string]string // Container labels
	CreatedAt int64             // Creation timestamp
}
