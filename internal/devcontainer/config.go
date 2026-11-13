// Package devcontainer parses, substitutes and resolves devcontainer.json
// into a ResolvedDevContainer, the input to everything that builds and runs
// the environment.
package devcontainer

import (
	"github.com/griffithind/dcx/internal/lifecyclecmd"
	"github.com/griffithind/dcx/internal/mount"
)

// PlanType identifies the execution plan type for a devcontainer.
type PlanType string

const (
	// PlanTypeImage indicates the devcontainer uses a pre-built image.
	PlanTypeImage PlanType = "image"

	// PlanTypeDockerfile indicates the devcontainer builds from a Dockerfile.
	PlanTypeDockerfile PlanType = "dockerfile"

	// PlanTypeCompose indicates the devcontainer uses Docker Compose.
	PlanTypeCompose PlanType = "compose"
)

// DevContainerConfig represents the parsed devcontainer.json configuration.
type DevContainerConfig struct {
	// Name is the display name for the dev container.
	Name string `json:"name,omitempty"`

	// Image-based configuration
	Image string `json:"image,omitempty"`

	// Dockerfile-based configuration
	Build *BuildConfig `json:"build,omitempty"`

	// Compose-based configuration
	DockerComposeFile StringOrSlice `json:"dockerComposeFile,omitempty"`
	Service           string        `json:"service,omitempty"`
	RunServices       []string      `json:"runServices,omitempty"`

	// Workspace configuration
	WorkspaceFolder string `json:"workspaceFolder,omitempty"`
	WorkspaceMount  string `json:"workspaceMount,omitempty"`

	// User configuration
	RemoteUser          string `json:"remoteUser,omitempty"`
	ContainerUser       string `json:"containerUser,omitempty"`
	UpdateRemoteUserUID *bool  `json:"updateRemoteUserUID,omitempty"` // Auto-update UID to match host user

	// Environment variables
	ContainerEnv map[string]string  `json:"containerEnv,omitempty"`
	RemoteEnv    map[string]*string `json:"remoteEnv,omitempty"` // null unsets the variable

	// Features
	Features                    map[string]interface{} `json:"features,omitempty"`
	OverrideFeatureInstallOrder []string               `json:"overrideFeatureInstallOrder,omitempty"`

	// Port forwarding
	ForwardPorts         PortList                 `json:"forwardPorts,omitempty"`
	AppPort              PortList                 `json:"appPort,omitempty"` // published application ports
	PortsAttributes      map[string]PortAttribute `json:"portsAttributes,omitempty"`
	OtherPortsAttributes *PortAttribute           `json:"otherPortsAttributes,omitempty"` // Default attributes for unlisted ports

	// Mounts - can be strings or objects
	Mounts []Mount `json:"mounts,omitempty"`

	// Docker run arguments
	RunArgs []string `json:"runArgs,omitempty"`

	// Lifecycle hooks
	InitializeCommand    *lifecyclecmd.Command `json:"initializeCommand,omitempty"`
	OnCreateCommand      *lifecyclecmd.Command `json:"onCreateCommand,omitempty"`
	UpdateContentCommand *lifecyclecmd.Command `json:"updateContentCommand,omitempty"`
	PostCreateCommand    *lifecyclecmd.Command `json:"postCreateCommand,omitempty"`
	PostStartCommand     *lifecyclecmd.Command `json:"postStartCommand,omitempty"`
	PostAttachCommand    *lifecyclecmd.Command `json:"postAttachCommand,omitempty"`
	WaitFor              string                `json:"waitFor,omitempty"` // Which lifecycle command to wait for (onCreateCommand, updateContentCommand, postCreateCommand, postStartCommand)

	// User environment probing
	UserEnvProbe string `json:"userEnvProbe,omitempty"` // How to probe user environment (none, loginShell, loginInteractiveShell, interactiveShell)

	// Runtime options
	OverrideCommand *bool    `json:"overrideCommand,omitempty"`
	ShutdownAction  string   `json:"shutdownAction,omitempty"`
	Init            *bool    `json:"init,omitempty"`
	Privileged      *bool    `json:"privileged,omitempty"`
	CapAdd          []string `json:"capAdd,omitempty"`
	SecurityOpt     []string `json:"securityOpt,omitempty"`

	// GPU support
	HostRequirements *HostRequirements `json:"hostRequirements,omitempty"`

	// Customizations for tools like VS Code
	Customizations map[string]interface{} `json:"customizations,omitempty"`

	// LegacyDockerFile and LegacyContext are the pre-"build" spellings of
	// build.dockerfile and build.context, still accepted by the reference
	// implementation; ParseFile moves them into Build.
	LegacyDockerFile string `json:"dockerFile,omitempty"`
	LegacyContext    string `json:"context,omitempty"`

	// ID and Entrypoint appear in devcontainer.metadata entries a feature
	// contributed (the feature's id and its entrypoint script); they are
	// not devcontainer.json properties.
	ID         string `json:"id,omitempty"`
	Entrypoint string `json:"entrypoint,omitempty"`
}

// PlanType returns the execution plan type for this configuration.
// This is the canonical method for determining how to build/run the container.
func (c *DevContainerConfig) PlanType() PlanType {
	if len(c.DockerComposeFile) > 0 {
		return PlanTypeCompose
	}
	if c.Build != nil {
		return PlanTypeDockerfile
	}
	return PlanTypeImage
}

// BuildConfig represents the build configuration for a devcontainer.
type BuildConfig struct {
	Dockerfile string            `json:"dockerfile,omitempty"`
	Context    string            `json:"context,omitempty"`
	Args       map[string]string `json:"args,omitempty"`
	Target     string            `json:"target,omitempty"`
	CacheFrom  []string          `json:"cacheFrom,omitempty"`
	Options    []string          `json:"options,omitempty"`
}

// HostRequirements specifies host machine requirements.
type HostRequirements struct {
	CPUs    int             `json:"cpus,omitempty"`
	Memory  string          `json:"memory,omitempty"`
	Storage string          `json:"storage,omitempty"`
	GPU     *GPURequirement `json:"gpu,omitempty"`
}

// IsComposePlan returns true if this config uses docker compose.
func (c *DevContainerConfig) IsComposePlan() bool {
	return len(c.DockerComposeFile) > 0
}

// IsSinglePlan returns true if this config uses image or build.
func (c *DevContainerConfig) IsSinglePlan() bool {
	return c.Image != "" || c.Build != nil
}

// PortAttribute represents attributes for a specific port.
type PortAttribute struct {
	Label            string `json:"label,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	OnAutoForward    string `json:"onAutoForward,omitempty"`
	RequireLocalPort bool   `json:"requireLocalPort,omitempty"`
	ElevateIfNeeded  bool   `json:"elevateIfNeeded,omitempty"`
}

// Mount is a mount specification (see package mount).
type Mount = mount.Mount

// ParseMountString parses a mount given as a string (see mount.Parse).
func ParseMountString(spec string) (Mount, error) { return mount.Parse(spec) }

// ParseWorkspaceMount parses a workspace mount string into a Mount, or
// returns nil if it is invalid.
func ParseWorkspaceMount(spec string) *Mount {
	m, err := ParseMountString(spec)
	if err != nil {
		return nil
	}
	return &m
}
