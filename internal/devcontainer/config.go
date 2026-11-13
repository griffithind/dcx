// Package devcontainer provides types and utilities for parsing, validating,
// and working with devcontainer.json configurations.
//
// This package is the core domain package for DCX, establishing "Dev Container"
// as the central concept. It replaces the previous internal/config package
// with clearer naming and organization.
package devcontainer

import (
	"encoding/json"
	"fmt"
	"strings"
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
	Service           string      `json:"service,omitempty"`
	RunServices       []string    `json:"runServices,omitempty"`

	// Workspace configuration
	WorkspaceFolder string `json:"workspaceFolder,omitempty"`
	WorkspaceMount  string `json:"workspaceMount,omitempty"`

	// User configuration
	RemoteUser          string `json:"remoteUser,omitempty"`
	ContainerUser       string `json:"containerUser,omitempty"`
	UpdateRemoteUserUID *bool  `json:"updateRemoteUserUID,omitempty"` // Auto-update UID to match host user

	// Environment variables
	ContainerEnv map[string]string `json:"containerEnv,omitempty"`
	RemoteEnv    map[string]string `json:"remoteEnv,omitempty"`

	// Features
	Features                    map[string]interface{} `json:"features,omitempty"`
	OverrideFeatureInstallOrder []string               `json:"overrideFeatureInstallOrder,omitempty"`

	// Port forwarding
	ForwardPorts         []interface{}          `json:"forwardPorts,omitempty"`
	AppPort              interface{}            `json:"appPort,omitempty"` // integer, string, or array - published application ports
	PortsAttributes      map[string]interface{} `json:"portsAttributes,omitempty"`
	OtherPortsAttributes interface{}            `json:"otherPortsAttributes,omitempty"` // Default attributes for unlisted ports

	// Mounts - can be strings or objects
	Mounts []Mount `json:"mounts,omitempty"`

	// Docker run arguments
	RunArgs []string `json:"runArgs,omitempty"`

	// Lifecycle hooks
	InitializeCommand    interface{} `json:"initializeCommand,omitempty"`
	OnCreateCommand      interface{} `json:"onCreateCommand,omitempty"`
	UpdateContentCommand interface{} `json:"updateContentCommand,omitempty"`
	PostCreateCommand    interface{} `json:"postCreateCommand,omitempty"`
	PostStartCommand     interface{} `json:"postStartCommand,omitempty"`
	PostAttachCommand    interface{} `json:"postAttachCommand,omitempty"`
	WaitFor              string      `json:"waitFor,omitempty"` // Which lifecycle command to wait for (onCreateCommand, updateContentCommand, postCreateCommand, postStartCommand)

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

	// Store the raw JSON for hash computation
	rawJSON []byte
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
	CPUs    int         `json:"cpus,omitempty"`
	Memory  string      `json:"memory,omitempty"`
	Storage string      `json:"storage,omitempty"`
	GPU     interface{} `json:"gpu,omitempty"` // bool or GPURequirement
}

// GetDockerComposeFiles returns the docker compose file paths as a slice.
func (c *DevContainerConfig) GetDockerComposeFiles() []string {
	return c.DockerComposeFile
}

// IsComposePlan returns true if this config uses docker compose.
func (c *DevContainerConfig) IsComposePlan() bool {
	return len(c.DockerComposeFile) > 0
}

// IsSinglePlan returns true if this config uses image or build.
func (c *DevContainerConfig) IsSinglePlan() bool {
	return c.Image != "" || c.Build != nil
}

// GetRawJSON returns the raw JSON content for hash computation.
func (c *DevContainerConfig) GetRawJSON() []byte {
	return c.rawJSON
}

// SetRawJSON stores the raw JSON content.
func (c *DevContainerConfig) SetRawJSON(data []byte) {
	c.rawJSON = data
}

// MarshalJSON implements json.Marshaler.
func (c *DevContainerConfig) MarshalJSON() ([]byte, error) {
	type Alias DevContainerConfig
	return json.Marshal((*Alias)(c))
}

// PortAttribute represents attributes for a specific port.
type PortAttribute struct {
	Label            string `json:"label,omitempty"`
	Protocol         string `json:"protocol,omitempty"`
	OnAutoForward    string `json:"onAutoForward,omitempty"`
	RequireLocalPort bool   `json:"requireLocalPort,omitempty"`
	ElevateIfNeeded  bool   `json:"elevateIfNeeded,omitempty"`
}

// Mount represents a mount specification that can be either a string or an object.
type Mount struct {
	Source      string `json:"source,omitempty"`
	Target      string `json:"target,omitempty"`
	Type        string `json:"type,omitempty"` // bind, volume, tmpfs
	ReadOnly    bool   `json:"readonly,omitempty"`
	Consistency string `json:"consistency,omitempty"` // cached, delegated, consistent (macOS)
	// Raw holds the original string if mount was specified as a string
	Raw string `json:"-"`
}

// UnmarshalJSON handles both string and object forms of mount specifications.
func (m *Mount) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		// Parsed provisionally so metadata merging can key on Target;
		// SubstituteConfig re-parses after variable substitution.
		if parsed, err := ParseMountString(s); err == nil {
			*m = parsed
		}
		m.Raw = s
		return nil
	}

	// Try object form
	type mountAlias Mount
	var obj mountAlias
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*m = Mount(obj)
	return nil
}

// String returns the mount as a docker-style string.
func (m Mount) String() string {
	if m.Raw != "" {
		return m.Raw
	}
	if m.Type == "" {
		m.Type = "bind"
	}
	result := fmt.Sprintf("type=%s,source=%s,target=%s", m.Type, m.Source, m.Target)
	if m.ReadOnly {
		result += ",readonly"
	}
	return result
}

// ParseMountString parses a mount given as a string. It is the only mount
// string parser in dcx (user, image-metadata and feature mounts all use it).
//
// Two forms are accepted:
//   - Docker --mount syntax: "type=bind,source=/a,target=/b,readonly"
//     (src/dst/destination aliases, readonly or readonly=true, consistency)
//   - Docker -v short syntax: "/host:/container[:ro]"
//
// The form is chosen by the presence of '=', so a value containing ':'
// (e.g. an expanded path) can never be mistaken for the short form.
func ParseMountString(spec string) (Mount, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Mount{}, fmt.Errorf("empty mount")
	}
	if strings.Contains(spec, "=") {
		return parseKeyValueMount(spec)
	}
	return parseShortMount(spec)
}

func parseShortMount(spec string) (Mount, error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return Mount{}, fmt.Errorf("invalid mount %q: expected source:target[:options]", spec)
	}
	m := Mount{Source: parts[0], Target: parts[1], Type: "bind"}
	if !strings.HasPrefix(m.Source, "/") && !strings.HasPrefix(m.Source, ".") && !strings.HasPrefix(m.Source, "~") {
		m.Type = "volume"
	}
	if len(parts) == 3 {
		for _, opt := range strings.Split(parts[2], ",") {
			switch opt {
			case "ro", "readonly":
				m.ReadOnly = true
			case "cached", "delegated", "consistent":
				m.Consistency = opt
			}
		}
	}
	return m, nil
}

func parseKeyValueMount(spec string) (Mount, error) {
	m := Mount{Type: "bind"}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, hasValue := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "source", "src":
			m.Source = value
		case "target", "dst", "destination":
			m.Target = value
		case "type":
			m.Type = value
		case "readonly", "ro":
			m.ReadOnly = !hasValue || value == "true" || value == "1"
		case "consistency":
			m.Consistency = value
		}
	}
	if m.Target == "" {
		return Mount{}, fmt.Errorf("invalid mount %q: missing target", spec)
	}
	if m.Type != "tmpfs" && m.Source == "" {
		return Mount{}, fmt.Errorf("invalid mount %q: missing source", spec)
	}
	return m, nil
}

// ParseWorkspaceMount parses a workspace mount string into a Mount, or
// returns nil if it is invalid.
func ParseWorkspaceMount(spec string) *Mount {
	m, err := ParseMountString(spec)
	if err != nil {
		return nil
	}
	return &m
}

// GetPortAttribute returns the attributes for a specific port.
func (c *DevContainerConfig) GetPortAttribute(port string) *PortAttribute {
	if c.PortsAttributes == nil {
		return nil
	}

	attr, ok := c.PortsAttributes[port]
	if !ok {
		return nil
	}

	attrMap, ok := attr.(map[string]interface{})
	if !ok {
		return nil
	}

	result := &PortAttribute{}
	if label, ok := attrMap["label"].(string); ok {
		result.Label = label
	}
	if protocol, ok := attrMap["protocol"].(string); ok {
		result.Protocol = protocol
	}
	if onAutoForward, ok := attrMap["onAutoForward"].(string); ok {
		result.OnAutoForward = onAutoForward
	}
	if requireLocalPort, ok := attrMap["requireLocalPort"].(bool); ok {
		result.RequireLocalPort = requireLocalPort
	}
	if elevateIfNeeded, ok := attrMap["elevateIfNeeded"].(bool); ok {
		result.ElevateIfNeeded = elevateIfNeeded
	}

	return result
}
