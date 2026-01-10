package devcontainer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// StringOrSlice handles fields that can be string or []string.
// Used for: dockerComposeFile, runServices, etc.
type StringOrSlice []string

// UnmarshalJSON handles both string and []string forms.
func (s *StringOrSlice) UnmarshalJSON(data []byte) error {
	// Try string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = []string{str}
		return nil
	}

	// Try array
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return fmt.Errorf("StringOrSlice: expected string or []string, got: %s", string(data))
	}
	*s = arr
	return nil
}

// MarshalJSON serializes back to JSON (single string if length 1).
func (s StringOrSlice) MarshalJSON() ([]byte, error) {
	if len(s) == 1 {
		return json.Marshal(s[0])
	}
	return json.Marshal([]string(s))
}

// LifecycleCommand handles all forms of lifecycle commands:
// - string: "echo hello"
// - []string: ["echo", "hello"]
// - map[string]string|[]string: {"task1": "cmd1", "task2": ["cmd2", "arg"]}
type LifecycleCommand struct {
	// Commands contains the parsed lifecycle command entries.
	Commands []LifecycleEntry
}

// LifecycleEntry represents a single lifecycle command entry.
type LifecycleEntry struct {
	// Name is the task name (empty for simple string/array commands).
	Name string
	// Command is the command string (for string form).
	Command string
	// Args is the command arguments (for array form).
	Args []string
}

// GetCommands returns all command strings, joining args with spaces if needed.
func (c *LifecycleCommand) GetCommands() []string {
	if c == nil {
		return nil
	}
	result := make([]string, 0, len(c.Commands))
	for _, entry := range c.Commands {
		if entry.Command != "" {
			result = append(result, entry.Command)
		} else if len(entry.Args) > 0 {
			result = append(result, strings.Join(entry.Args, " "))
		}
	}
	return result
}

// UnmarshalJSON handles string, []string, and map forms.
func (c *LifecycleCommand) UnmarshalJSON(data []byte) error {
	// Try string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		c.Commands = []LifecycleEntry{{Command: str}}
		return nil
	}

	// Try array of strings
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		c.Commands = []LifecycleEntry{{Args: arr}}
		return nil
	}

	// Try map (parallel commands)
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err == nil {
		c.Commands = make([]LifecycleEntry, 0, len(m))
		for name, cmd := range m {
			entry := LifecycleEntry{Name: name}
			switch v := cmd.(type) {
			case string:
				entry.Command = v
			case []interface{}:
				entry.Args = make([]string, 0, len(v))
				for _, item := range v {
					if s, ok := item.(string); ok {
						entry.Args = append(entry.Args, s)
					}
				}
			}
			c.Commands = append(c.Commands, entry)
		}
		return nil
	}

	return fmt.Errorf("LifecycleCommand: expected string, []string, or map, got: %s", string(data))
}

// MarshalJSON serializes the lifecycle command back to JSON.
func (c LifecycleCommand) MarshalJSON() ([]byte, error) {
	if len(c.Commands) == 0 {
		return json.Marshal(nil)
	}
	if len(c.Commands) == 1 && c.Commands[0].Name == "" {
		// Single command without name
		entry := c.Commands[0]
		if entry.Command != "" {
			return json.Marshal(entry.Command)
		}
		return json.Marshal(entry.Args)
	}
	// Multiple commands or named commands - use map
	m := make(map[string]interface{}, len(c.Commands))
	for _, entry := range c.Commands {
		name := entry.Name
		if name == "" {
			name = "default"
		}
		if entry.Command != "" {
			m[name] = entry.Command
		} else {
			m[name] = entry.Args
		}
	}
	return json.Marshal(m)
}

// PortSpec handles port specifications.
// Can be: int (5000), string ("5000:3000"), or object with detailed config.
type PortSpec struct {
	// Container is the container port.
	Container int
	// Host is the host port (defaults to Container if not specified).
	Host int
	// Label is an optional label for the port.
	Label string
	// Protocol is "tcp" or "udp" (defaults to "tcp").
	Protocol string
	// OnAutoForward specifies behavior when port is auto-forwarded.
	OnAutoForward string
}

// UnmarshalJSON handles int, string, and object forms.
func (p *PortSpec) UnmarshalJSON(data []byte) error {
	// Try int/float
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		p.Container = int(num)
		p.Host = p.Container
		p.Protocol = "tcp"
		return nil
	}

	// Try string (format: "host:container" or "container")
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		return p.parseString(str)
	}

	// Try object
	var obj struct {
		ContainerPort int    `json:"containerPort"`
		HostPort      int    `json:"hostPort"`
		Label         string `json:"label"`
		Protocol      string `json:"protocol"`
		OnAutoForward string `json:"onAutoForward"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		p.Container = obj.ContainerPort
		p.Host = obj.HostPort
		if p.Host == 0 {
			p.Host = p.Container
		}
		p.Label = obj.Label
		p.Protocol = obj.Protocol
		if p.Protocol == "" {
			p.Protocol = "tcp"
		}
		p.OnAutoForward = obj.OnAutoForward
		return nil
	}

	return fmt.Errorf("PortSpec: expected int, string, or object, got: %s", string(data))
}

func (p *PortSpec) parseString(s string) error {
	p.Protocol = "tcp"

	// Handle protocol suffix
	if idx := strings.LastIndex(s, "/"); idx != -1 {
		p.Protocol = s[idx+1:]
		s = s[:idx]
	}

	// Handle host:container format
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		port, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("PortSpec: invalid port number: %s", parts[0])
		}
		p.Container = port
		p.Host = port
	case 2:
		host, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("PortSpec: invalid host port: %s", parts[0])
		}
		container, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("PortSpec: invalid container port: %s", parts[1])
		}
		p.Host = host
		p.Container = container
	default:
		return fmt.Errorf("PortSpec: invalid format: %s", s)
	}
	return nil
}

// MarshalJSON serializes the port spec.
func (p PortSpec) MarshalJSON() ([]byte, error) {
	// Simple case: just the port number
	if p.Host == p.Container && p.Label == "" && p.Protocol == "tcp" && p.OnAutoForward == "" {
		return json.Marshal(p.Container)
	}
	// Complex case: use object form
	return json.Marshal(map[string]interface{}{
		"containerPort": p.Container,
		"hostPort":      p.Host,
		"label":         p.Label,
		"protocol":      p.Protocol,
		"onAutoForward": p.OnAutoForward,
	})
}

// String returns a docker-style port string.
func (p PortSpec) String() string {
	if p.Host == p.Container {
		if p.Protocol != "" && p.Protocol != "tcp" {
			return fmt.Sprintf("%d/%s", p.Container, p.Protocol)
		}
		return strconv.Itoa(p.Container)
	}
	if p.Protocol != "" && p.Protocol != "tcp" {
		return fmt.Sprintf("%d:%d/%s", p.Host, p.Container, p.Protocol)
	}
	return fmt.Sprintf("%d:%d", p.Host, p.Container)
}

// GPUConfig handles GPU requirements.
// Can be: bool, string ("all"), int (count), or object with detailed config.
type GPUConfig struct {
	// Enabled indicates whether GPU support is enabled.
	Enabled bool
	// Count is the number of GPUs (-1 means "all").
	Count int
	// Cores is the number of GPU cores (optional).
	Cores int
	// Memory is the GPU memory requirement (optional).
	Memory string
}

// UnmarshalJSON handles bool, string, int, and object forms.
func (g *GPUConfig) UnmarshalJSON(data []byte) error {
	// Try bool
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		g.Enabled = b
		if b {
			g.Count = -1 // -1 means "all"
		}
		return nil
	}

	// Try string
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		g.Enabled = true
		if str == "all" {
			g.Count = -1
		} else if count, err := strconv.Atoi(str); err == nil {
			g.Count = count
		}
		return nil
	}

	// Try int/float
	var num float64
	if err := json.Unmarshal(data, &num); err == nil {
		g.Enabled = num > 0
		g.Count = int(num)
		return nil
	}

	// Try object
	var obj struct {
		Count  interface{} `json:"count"`
		Cores  int         `json:"cores"`
		Memory string      `json:"memory"`
	}
	if err := json.Unmarshal(data, &obj); err == nil {
		g.Enabled = true
		switch v := obj.Count.(type) {
		case float64:
			g.Count = int(v)
		case string:
			if v == "all" {
				g.Count = -1
			} else if count, err := strconv.Atoi(v); err == nil {
				g.Count = count
			}
		}
		g.Cores = obj.Cores
		g.Memory = obj.Memory
		return nil
	}

	return fmt.Errorf("GPUConfig: expected bool, string, int, or object, got: %s", string(data))
}

// MarshalJSON serializes the GPU config.
func (g GPUConfig) MarshalJSON() ([]byte, error) {
	if !g.Enabled {
		return json.Marshal(false)
	}
	if g.Cores == 0 && g.Memory == "" {
		if g.Count == -1 {
			return json.Marshal("all")
		}
		return json.Marshal(g.Count)
	}
	return json.Marshal(map[string]interface{}{
		"count":  g.Count,
		"cores":  g.Cores,
		"memory": g.Memory,
	})
}

// FeatureConfig represents a single feature configuration.
// Features in devcontainer.json are specified as:
//   - "ghcr.io/feature/name:version": true  (enabled with defaults)
//   - "ghcr.io/feature/name:version": {"option": "value"}  (with options)
type FeatureConfig struct {
	// ID is the feature identifier (e.g., "ghcr.io/devcontainers/features/go:1").
	ID string
	// Enabled indicates whether the feature is enabled.
	Enabled bool
	// Options contains feature-specific options.
	Options map[string]interface{}
}

// ParseFeatures converts a map[string]interface{} to a slice of FeatureConfig.
// This preserves the feature ID as a field rather than a map key.
func ParseFeatures(features map[string]interface{}) []FeatureConfig {
	if features == nil {
		return nil
	}
	result := make([]FeatureConfig, 0, len(features))
	for id, opts := range features {
		cfg := FeatureConfig{ID: id}
		switch v := opts.(type) {
		case bool:
			cfg.Enabled = v
		case map[string]interface{}:
			cfg.Enabled = true
			cfg.Options = v
		default:
			cfg.Enabled = true
			cfg.Options = map[string]interface{}{"value": opts}
		}
		result = append(result, cfg)
	}
	return result
}

// PortSpecs is a slice of PortSpec with custom JSON handling.
type PortSpecs []PortSpec

// UnmarshalJSON handles []interface{} containing ints, strings, or objects.
func (ps *PortSpecs) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*ps = make([]PortSpec, 0, len(raw))
	for _, item := range raw {
		var spec PortSpec
		if err := json.Unmarshal(item, &spec); err != nil {
			return err
		}
		*ps = append(*ps, spec)
	}
	return nil
}
