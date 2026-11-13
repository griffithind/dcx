package devcontainer

import (
	"github.com/griffithind/dcx/internal/features"
)

// ResolvedDevContainer represents a fully resolved devcontainer configuration.
// This is the central domain object containing everything needed to create/run a container.
//
// It is produced only by ResolveWorkspace. Read resolved fields; Config is
// for display and for the lifecycle commands (which become resolved hook
// lists once they are typed).
type ResolvedDevContainer struct {
	// Identity is the workspace's ID, name, Docker resource name and SSH
	// alias (see NewIdentity).
	Identity

	// ConfigPath is the absolute path to devcontainer.json.
	ConfigPath string

	// ConfigDir is the directory containing devcontainer.json.
	ConfigDir string

	// LocalRoot is the workspace root directory on the host.
	LocalRoot string

	// === Source Configuration ===

	// Config is the effective configuration: devcontainer.json (substituted)
	// with the base image's devcontainer.metadata merged in.
	Config *DevContainerConfig

	// Local is devcontainer.json as written (substituted, before image
	// metadata was merged in). It is the staleness key's input and the
	// "local" entry dcx adds to an image's devcontainer.metadata label.
	Local *DevContainerConfig

	// === Execution Plan ===

	// Plan defines what needs to be built/run.
	// This is a type-safe interface: ImagePlan, DockerfilePlan, or ComposePlan.
	Plan ExecutionPlan

	// === Resolved Runtime Configuration ===
	// These are the final values after variable substitution and feature merging.

	// BaseImage is the original base image (before features).
	// For ImagePlan: the image specified in config
	// For DockerfilePlan: determined after building
	// For ComposePlan: extracted from service config
	BaseImage string

	// WorkspaceFolder is the path inside the container.
	WorkspaceFolder string

	// WorkspaceMount is the mount specification for the workspace.
	WorkspaceMount string

	// === User Configuration ===

	// RemoteUser is the user for remote operations.
	RemoteUser string

	// ContainerUser is the user for container creation.
	ContainerUser string

	// UpdateRemoteUserUID indicates whether to update UID to match host user.
	UpdateRemoteUserUID bool

	// EffectiveUser is the resolved user (RemoteUser or ContainerUser).
	EffectiveUser string

	// HostUID is the host user's UID.
	HostUID int

	// HostGID is the host user's GID.
	HostGID int

	// === Environment ===

	// ContainerEnv is set at container creation.
	ContainerEnv map[string]string

	// RemoteEnv is set in shell sessions.
	RemoteEnv map[string]string

	// === Runtime Options ===

	// Mounts are the volume mounts for the container.
	Mounts []Mount

	// CapAdd are Linux capabilities to add.
	CapAdd []string

	// SecurityOpt are security options.
	SecurityOpt []string

	// Privileged indicates if the container runs in privileged mode.
	Privileged bool

	// Init indicates if an init process should be used.
	Init bool

	// RunArgs contains parsed docker run arguments from devcontainer.json.
	RunArgs *ParsedRunArgs

	// OverrideCommand replaces the image/service command with a keep-alive
	// (overrideCommand; defaults to true for image and Dockerfile plans and
	// false for compose, per the spec).
	OverrideCommand bool

	// HostRequirements are the minimum host resources (nil: none).
	HostRequirements *HostRequirements

	// UserEnvProbe is how the user environment is probed for hooks and
	// sessions ("" or "none": not at all).
	UserEnvProbe string

	// === Ports ===

	// ForwardPorts are ports to forward from the container.
	ForwardPorts []PortForward

	// AppPorts are application ports to expose.
	AppPorts []PortForward

	// === Features ===

	// Features are the resolved and ordered features for installation.
	Features []*features.Feature

	// === Hash ===

	// ConfigHash is the combined hash of all build inputs (devcontainer.json,
	// Dockerfiles, compose files, features). Used for staleness detection
	// and image cache tagging.
	ConfigHash string

	// === GPU ===

	// GPURequirements specifies GPU requirements.
	GPURequirements *GPURequirements

	// === Secrets ===

	// RuntimeSecrets are secrets to mount at /run/secrets/<name> at runtime.
	// Map of secret name to config (command to fetch value).
	RuntimeSecrets map[string]SecretConfig

	// BuildSecrets are secrets for Docker BuildKit during builds.
	// Map of secret name to config (command to fetch value).
	BuildSecrets map[string]SecretConfig

	// === Build State ===

	// ShouldUpdateUID indicates whether UID update layer is needed.
	ShouldUpdateUID bool
}

// PortForward represents a port forwarding configuration.
type PortForward struct {
	HostPort      int
	ContainerPort int
	Host          string // Host to bind to (e.g., "localhost" for localhost-only)
	Protocol      string

	// EphemeralHostPort asks Docker to assign the host port itself (the
	// `host::container` syntax).
	EphemeralHostPort bool
}

// GPURequirements specifies GPU requirements for the container.
type GPURequirements struct {
	Enabled bool
	// Optional means the container should get GPUs only when the host has
	// them ("gpu": "optional"); without one it starts without GPUs.
	Optional bool
	Count    int
	Memory   string
	Cores    int
}

// ParsedRunArgs contains parsed values from devcontainer.json runArgs.
// This allows runArgs like "--network=host" or "--device /dev/foo" to be
// properly applied to container creation.
type ParsedRunArgs struct {
	User        string
	NetworkMode string
	IpcMode     string
	PidMode     string
	ShmSize     int64
	CapDrop     []string
	Devices     []string
	ExtraHosts  []string
	Sysctls     map[string]string

	// Passthrough holds every other flag, verbatim, for `docker run`.
	Passthrough []string
	// Ignored holds flags dcx refuses because they break container
	// management (--rm, --name, -d/--detach).
	Ignored []string
}

// NewResolvedDevContainer creates a new ResolvedDevContainer with initialized maps.
func NewResolvedDevContainer() *ResolvedDevContainer {
	return &ResolvedDevContainer{
		ContainerEnv:   make(map[string]string),
		RemoteEnv:      make(map[string]string),
		RuntimeSecrets: make(map[string]SecretConfig),
		BuildSecrets:   make(map[string]SecretConfig),
	}
}
