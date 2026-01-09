package parse

import (
	"strconv"
	"strings"
)

// PortBinding represents a parsed port binding specification.
type PortBinding struct {
	HostIP        string // Optional host IP (e.g., "127.0.0.1")
	HostPort      string // Host port number
	ContainerPort string // Container port number
	Protocol      string // Protocol: "tcp" or "udp"
}

// ParsePortBinding parses a single port specification.
// Supported formats:
//   - "8080" - Container port only, binds to same host port
//   - "8080:80" - hostPort:containerPort
//   - "127.0.0.1:8080:80" - hostIP:hostPort:containerPort
//   - "8080/udp" - Container port with protocol
//   - "8080:80/udp" - Full specification with protocol
func ParsePortBinding(spec string) *PortBinding {
	if spec == "" {
		return nil
	}

	pb := &PortBinding{Protocol: "tcp"}

	// Extract protocol suffix
	if idx := strings.LastIndex(spec, "/"); idx != -1 {
		pb.Protocol = spec[idx+1:]
		spec = spec[:idx]
	}

	parts := strings.Split(spec, ":")
	switch len(parts) {
	case 1:
		// Just container port, bind to same host port
		pb.ContainerPort = parts[0]
		pb.HostPort = parts[0]
	case 2:
		// hostPort:containerPort
		pb.HostPort = parts[0]
		pb.ContainerPort = parts[1]
	case 3:
		// hostIP:hostPort:containerPort
		pb.HostIP = parts[0]
		pb.HostPort = parts[1]
		pb.ContainerPort = parts[2]
	default:
		return nil
	}

	// Validate container port is numeric
	if _, err := strconv.Atoi(pb.ContainerPort); err != nil {
		return nil
	}

	return pb
}

// ParsePortBindings parses multiple port specifications.
// Invalid specifications are skipped.
func ParsePortBindings(specs []string) []*PortBinding {
	result := make([]*PortBinding, 0, len(specs))
	for _, spec := range specs {
		if pb := ParsePortBinding(spec); pb != nil {
			result = append(result, pb)
		}
	}
	return result
}

// String returns the port binding in a standard format for display.
func (p *PortBinding) String() string {
	if p == nil {
		return ""
	}

	var result string
	if p.HostIP != "" {
		result = p.HostIP + ":" + p.HostPort + ":" + p.ContainerPort
	} else if p.HostPort != p.ContainerPort {
		result = p.HostPort + ":" + p.ContainerPort
	} else {
		result = p.ContainerPort
	}

	if p.Protocol != "tcp" {
		result += "/" + p.Protocol
	}

	return result
}
