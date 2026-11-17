package devcontainer

import (
	"github.com/griffithind/dcx/internal/features"
)

// ResolvedDevContainer represents a fully resolved devcontainer configuration.
// This is the central domain object containing everything needed to create/run a container.
//
// It is produced only by ResolveWorkspace. Read resolved fields; Config is
// for display.
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

	// Raw is devcontainer.json as written, before variable substitution:
	// the input of the staleness key (ConfigHash).
	Raw *DevContainerConfig

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

	// BaseImage is the image of an image plan ("" for Dockerfile and
	// compose plans; see MetadataImage for the image they build FROM).
	BaseImage string

	// WorkspaceFolder is the path inside the container.
	WorkspaceFolder string

	// WorkspaceMount is the workspace mount of an image or Dockerfile plan
	// (nil for none, and for compose): workspaceMount, or the bind of the
	// git repository containing the workspace (see workspace.go).
	WorkspaceMount *Mount

	// === User Configuration ===

	// RemoteUser is the user for remote operations.
	RemoteUser string

	// ContainerUser is the user for container creation.
	ContainerUser string

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

	// RemoteEnvUnset are variables remoteEnv sets to null: removed from
	// sessions.
	RemoteEnvUnset []string

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

	// Hooks are the lifecycle commands of every phase (image metadata,
	// features, devcontainer.json).
	Hooks Hooks

	// Entrypoints are the scripts run when the container starts (image
	// metadata, then features; see resolveEntrypoints).
	Entrypoints []string

	// MetadataImage is the image whose devcontainer.metadata applies: the
	// image, or the base image a Dockerfile or compose service builds FROM
	// ("" when unknown). MetadataLabel is that label as read ("" when the
	// image has none or was not available).
	MetadataImage string
	MetadataLabel string

	// === Features ===

	// Features are the resolved and ordered features for installation.
	Features []*features.Feature

	// === Hash ===

	// ConfigHash is the staleness key: the files the user edits (see
	// ComputeConfigHash). A change recreates the container.
	ConfigHash string

	// InputsHash covers everything that determines the container,
	// substituted values and feature digests included (ComputeInputsHash).
	// When only it changes, dcx reports drift and keeps the container.
	InputsHash string

	// === GPU ===

	// GPU is the GPU requirement (nil: no GPUs).
	GPU *GPURequirement

	// === Secrets ===

	// RuntimeSecrets are secrets to mount at /run/secrets/<name> at runtime.
	// Map of secret name to config (command to fetch value).
	RuntimeSecrets map[string]SecretConfig

	// BuildSecrets are secrets for Docker BuildKit during builds.
	// Map of secret name to config (command to fetch value).
	BuildSecrets map[string]SecretConfig

	// === Build State ===

	// ShouldUpdateUID is whether the UID update layer is built (see
	// ShouldUpdateUID, the function).
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

// ParsedRunArgs contains parsed values from devcontainer.json runArgs.
// This allows runArgs like "--network=host" or "--device /dev/foo" to be
// properly applied to container creation.
type ParsedRunArgs struct {
	User        string
	Name        string // --name: the container name (dcx finds containers by label)
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
	// management (--rm, -d/--detach).
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
