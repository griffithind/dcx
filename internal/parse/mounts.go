// Package parse provides shared parsing utilities for devcontainer configurations.
package parse

import "strings"

// Mount represents a parsed mount specification.
type Mount struct {
	Source   string
	Target   string
	Type     string // bind, volume, tmpfs
	ReadOnly bool
}

// ParseMount parses a devcontainer mount string into a Mount struct.
// Devcontainer format: "source=/path,target=/path,type=bind,consistency=cached,readonly=true"
// Also accepts Docker short format: "source:target" or "source:target:ro"
func ParseMount(mount string) *Mount {
	// Check for Docker short format (contains colon but no source= pattern)
	if strings.Contains(mount, ":") && !strings.Contains(mount, "source=") {
		return parseDockerShortMount(mount)
	}

	return parseDevcontainerMount(mount)
}

// parseDockerShortMount parses Docker short format: "source:target" or "source:target:ro"
func parseDockerShortMount(mount string) *Mount {
	parts := strings.SplitN(mount, ":", 3)
	if len(parts) < 2 {
		return nil
	}

	m := &Mount{
		Source: parts[0],
		Target: parts[1],
		Type:   "bind",
	}

	if len(parts) >= 3 {
		// Check for readonly flag
		opts := strings.Split(parts[2], ",")
		for _, opt := range opts {
			if opt == "ro" || opt == "readonly" {
				m.ReadOnly = true
			}
		}
	}

	return m
}

// parseDevcontainerMount parses devcontainer key=value format.
func parseDevcontainerMount(mount string) *Mount {
	parts := strings.Split(mount, ",")

	m := &Mount{
		Type: "bind", // Default type
	}

	for _, part := range parts {
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

	// Validate required fields
	if m.Target == "" {
		return nil
	}
	if m.Type != "tmpfs" && m.Source == "" {
		return nil
	}

	return m
}

// ToDockerFormat returns the mount in Docker CLI format: "source:target[:ro]"
func (m *Mount) ToDockerFormat() string {
	if m == nil {
		return ""
	}

	result := m.Source + ":" + m.Target
	if m.ReadOnly {
		result += ":ro"
	}
	return result
}

// ToDockerFormatWithSuffix returns the mount with an optional suffix (e.g., ":Z" for SELinux).
func (m *Mount) ToDockerFormatWithSuffix(suffix string) string {
	if m == nil {
		return ""
	}

	result := m.Source + ":" + m.Target
	if m.ReadOnly && suffix != "" {
		result += ":ro" + suffix
	} else if m.ReadOnly {
		result += ":ro"
	} else if suffix != "" {
		result += suffix
	}
	return result
}

// ToComposeFormat returns the mount in Docker Compose format.
// For bind mounts: "source:target[:suffix]"
// For volume mounts: "volumeName:target"
// For tmpfs mounts: not supported, returns empty
func (m *Mount) ToComposeFormat(suffix string) string {
	if m == nil {
		return ""
	}

	switch m.Type {
	case "bind":
		return m.ToDockerFormatWithSuffix(suffix)
	case "volume":
		result := m.Source + ":" + m.Target
		if m.ReadOnly {
			result += ":ro"
		}
		return result
	case "tmpfs":
		// Compose handles tmpfs differently
		return "tmpfs:" + m.Target
	default:
		return ""
	}
}
