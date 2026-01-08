// Package config handles parsing and validation of devcontainer.json files.
package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DevcontainerConfig represents the parsed devcontainer.json configuration.
type DevcontainerConfig struct {
	// Name is the display name for the dev container.
	Name string `json:"name,omitempty"`

	// Image-based configuration
	Image string `json:"image,omitempty"`

	// Dockerfile-based configuration
	Build *BuildConfig `json:"build,omitempty"`

	// Compose-based configuration
	DockerComposeFile interface{} `json:"dockerComposeFile,omitempty"` // string or []string
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
	OverrideCommand   *bool    `json:"overrideCommand,omitempty"`
	ShutdownAction    string   `json:"shutdownAction,omitempty"`
	Init              *bool    `json:"init,omitempty"`
	Privileged        *bool    `json:"privileged,omitempty"`
	CapAdd            []string `json:"capAdd,omitempty"`
	SecurityOpt       []string `json:"securityOpt,omitempty"`

	// GPU support
	HostRequirements *HostRequirements `json:"hostRequirements,omitempty"`

	// Customizations for tools like VS Code
	Customizations map[string]interface{} `json:"customizations,omitempty"`

	// Store the raw JSON for hash computation
	rawJSON []byte
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
	CPUs   int    `json:"cpus,omitempty"`
	Memory string `json:"memory,omitempty"`
	Storage string `json:"storage,omitempty"`
	GPU    interface{} `json:"gpu,omitempty"` // bool or GPURequirement
}

// GetDockerComposeFiles returns the docker compose file paths as a slice.
func (c *DevcontainerConfig) GetDockerComposeFiles() []string {
	if c.DockerComposeFile == nil {
		return nil
	}

	switch v := c.DockerComposeFile.(type) {
	case string:
		return []string{v}
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return v
	default:
		return nil
	}
}

// IsComposePlan returns true if this config uses docker compose.
func (c *DevcontainerConfig) IsComposePlan() bool {
	return c.DockerComposeFile != nil
}

// IsSinglePlan returns true if this config uses image or build.
func (c *DevcontainerConfig) IsSinglePlan() bool {
	return c.Image != "" || c.Build != nil
}

// GetRawJSON returns the raw JSON content for hash computation.
func (c *DevcontainerConfig) GetRawJSON() []byte {
	return c.rawJSON
}

// SetRawJSON stores the raw JSON content.
func (c *DevcontainerConfig) SetRawJSON(data []byte) {
	c.rawJSON = data
}

// MarshalJSON implements json.Marshaler.
func (c *DevcontainerConfig) MarshalJSON() ([]byte, error) {
	type Alias DevcontainerConfig
	return json.Marshal((*Alias)(c))
}

// GetForwardPorts returns the forward ports as a slice of strings.
// Each element is in the format "hostPort:containerPort" or just "port".
func (c *DevcontainerConfig) GetForwardPorts() []string {
	if len(c.ForwardPorts) == 0 {
		return nil
	}

	result := make([]string, 0, len(c.ForwardPorts))
	for _, port := range c.ForwardPorts {
		switch v := port.(type) {
		case float64:
			// Single port number
			result = append(result, formatPort(int(v)))
		case int:
			result = append(result, formatPort(v))
		case string:
			// Already a string (could be "8080" or "8080:80" or "host:container")
			result = append(result, v)
		}
	}

	return result
}

func formatPort(port int) string {
	// For devcontainer ports, we expose on the same host port
	return fmt.Sprintf("%d:%d", port, port)
}

// GetAppPorts returns the app ports as a slice of strings.
// AppPort can be an integer, string, or array of integers/strings.
// Each element is in the format "hostPort:containerPort".
func (c *DevcontainerConfig) GetAppPorts() []string {
	if c.AppPort == nil {
		return nil
	}

	var result []string

	switch v := c.AppPort.(type) {
	case float64:
		// Single port number
		result = append(result, formatPort(int(v)))
	case int:
		result = append(result, formatPort(v))
	case string:
		// Single port as string
		result = append(result, v)
	case []interface{}:
		// Array of ports
		for _, port := range v {
			switch p := port.(type) {
			case float64:
				result = append(result, formatPort(int(p)))
			case int:
				result = append(result, formatPort(p))
			case string:
				result = append(result, p)
			}
		}
	}

	return result
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
	Source   string `json:"source,omitempty"`
	Target   string `json:"target,omitempty"`
	Type     string `json:"type,omitempty"`
	ReadOnly bool   `json:"readonly,omitempty"`
	// Raw holds the original string if mount was specified as a string
	Raw string `json:"-"`
}

// UnmarshalJSON handles both string and object forms of mount specifications.
func (m *Mount) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.Raw = s
		// Parse the mount string: "source=...,target=...,type=...,readonly"
		parts := strings.Split(s, ",")
		for _, part := range parts {
			if part == "readonly" || part == "ro" {
				m.ReadOnly = true
				continue
			}
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			switch key {
			case "source", "src":
				m.Source = value
			case "target", "dst", "destination":
				m.Target = value
			case "type":
				m.Type = value
			case "readonly", "ro":
				m.ReadOnly = value == "true" || value == "1"
			}
		}
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

// GetPortAttribute returns the attributes for a specific port.
func (c *DevcontainerConfig) GetPortAttribute(port string) *PortAttribute {
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
