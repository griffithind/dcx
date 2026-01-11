package devcontainer

import (
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/state"
)

// ResolvedDevContainer represents a fully resolved devcontainer configuration.
// This is the central domain object containing everything needed to create/run a container.
//
// This struct replaces the previous Workspace + ResolvedConfig nested structure,
// flattening all fields into a single coherent type aligned with devcontainer terminology.
type ResolvedDevContainer struct {
	// === Identity ===

	// ID is the stable identifier: base32(sha256(realpath))[0:12]
	ID string

	// Name is the human-readable name (from config or directory name).
	Name string

	// ConfigPath is the absolute path to devcontainer.json.
	ConfigPath string

	// ConfigDir is the directory containing devcontainer.json.
	ConfigDir string

	// LocalRoot is the workspace root directory on the host.
	LocalRoot string

	// === Source Configuration ===

	// RawConfig is the original parsed devcontainer.json configuration.
	RawConfig *DevContainerConfig

	// === Execution Plan ===

	// Plan defines what needs to be built/run.
	// This is a type-safe interface: ImagePlan, DockerfilePlan, or ComposePlan.
	Plan ExecutionPlan

	// === Resolved Runtime Configuration ===
	// These are the final values after variable substitution and feature merging.

	// Image is the final image to use (after feature derivation).
	Image string

	// BaseImage is the original base image (before features).
	BaseImage string

	// ServiceName is the container/service name (sanitized for Docker).
	ServiceName string

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
	Mounts []mount.Mount

	// CapAdd are Linux capabilities to add.
	CapAdd []string

	// SecurityOpt are security options.
	SecurityOpt []string

	// Privileged indicates if the container runs in privileged mode.
	Privileged bool

	// Init indicates if an init process should be used.
	Init bool

	// RunArgs are additional docker run arguments.
	RunArgs []string

	// === Ports ===

	// ForwardPorts are ports to forward from the container.
	ForwardPorts []PortForward

	// AppPorts are application ports to expose.
	AppPorts []PortForward

	// === Features ===

	// Features are the resolved and ordered features for installation.
	Features []*features.Feature

	// === Lifecycle ===

	// Lifecycle contains all lifecycle hook commands.
	Lifecycle *LifecycleHooks

	// === Hashes ===

	// Hashes are computed hashes for staleness detection.
	Hashes *ContentHashes

	// === Customizations ===

	// Customizations are tool-specific customizations (e.g., VS Code settings).
	Customizations map[string]interface{}

	// === GPU ===

	// GPURequirements specifies GPU requirements.
	GPURequirements *GPURequirements

	// === Build Plan ===

	// NeedsBuild indicates whether any build is required.
	NeedsBuild bool

	// BuildReason explains why build is needed (new, stale, forced).
	BuildReason string

	// DerivedImage is the derived image name with features.
	DerivedImage string

	// ShouldUpdateUID indicates whether UID update layer is needed.
	ShouldUpdateUID bool

	// === Labels ===

	// Labels are the container labels to apply.
	Labels *state.ContainerLabels
}

// PortForward represents a port forwarding configuration.
type PortForward struct {
	HostPort      int
	ContainerPort int
	Label         string
	Protocol      string
	OnAutoForward string
}

// LifecycleHooks contains all lifecycle hook commands.
type LifecycleHooks struct {
	Initialize    []HookCommand
	OnCreate      []HookCommand
	UpdateContent []HookCommand
	PostCreate    []HookCommand
	PostStart     []HookCommand
	PostAttach    []HookCommand
	WaitFor       string // Which hook to wait for before considering ready

	// Feature hooks (run before devcontainer hooks)
	FeatureOnCreate      []HookCommand
	FeatureUpdateContent []HookCommand
	FeaturePostCreate    []HookCommand
	FeaturePostStart     []HookCommand
	FeaturePostAttach    []HookCommand
}

// HookCommand represents a single lifecycle hook command.
type HookCommand struct {
	Name     string   // Command name (for map-style commands)
	Command  string   // Shell command (if string format)
	Args     []string // Command args (if array format)
	Parallel bool     // Run in parallel with other named commands
}

// GPURequirements specifies GPU requirements for the container.
type GPURequirements struct {
	Enabled bool
	Count   int
	Memory  string
	Cores   int
}

// BuildPlan represents what needs to be built.
type BuildPlan struct {
	NeedsBuild      bool      // Whether any build is required
	BuildReason     string    // Why build is needed (new, stale, forced)
	ImageToBuild    string    // Image name to build
	DerivedImage    string    // Derived image with features
	BaseImage       string    // Original base image
	BuildTimestamp  time.Time // When build was last performed
	ShouldUpdateUID bool      // Whether UID update layer is needed
}

// NewResolvedDevContainer creates a new ResolvedDevContainer with initialized maps.
func NewResolvedDevContainer() *ResolvedDevContainer {
	return &ResolvedDevContainer{
		ContainerEnv:   make(map[string]string),
		RemoteEnv:      make(map[string]string),
		Customizations: make(map[string]interface{}),
		Hashes:         NewContentHashes(),
		Labels:         state.NewContainerLabels(),
	}
}
