package docker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/docker/go-connections/nat"
)

// parsePortBindings parses port specifications into exposed ports and port bindings.
// Supports formats: "8080", "8080:80", "127.0.0.1:8080:80", "8080/udp"
func parsePortBindings(ports []string) (nat.PortSet, nat.PortMap) {
	exposedPorts := make(nat.PortSet)
	portBindings := make(nat.PortMap)

	for _, portSpec := range ports {
		hostIP := ""
		hostPort := ""
		containerPort := ""
		protocol := "tcp"

		// Check for protocol suffix
		if idx := strings.LastIndex(portSpec, "/"); idx != -1 {
			protocol = portSpec[idx+1:]
			portSpec = portSpec[:idx]
		}

		parts := strings.Split(portSpec, ":")
		switch len(parts) {
		case 1:
			// Just container port, bind to same host port
			containerPort = parts[0]
			hostPort = parts[0]
		case 2:
			// hostPort:containerPort
			hostPort = parts[0]
			containerPort = parts[1]
		case 3:
			// hostIP:hostPort:containerPort
			hostIP = parts[0]
			hostPort = parts[1]
			containerPort = parts[2]
		default:
			continue
		}

		// Validate port numbers
		if _, err := strconv.Atoi(containerPort); err != nil {
			continue
		}

		natPort := nat.Port(fmt.Sprintf("%s/%s", containerPort, protocol))
		exposedPorts[natPort] = struct{}{}
		portBindings[natPort] = []nat.PortBinding{
			{
				HostIP:   hostIP,
				HostPort: hostPort,
			},
		}
	}

	return exposedPorts, portBindings
}

// parseMountSpec parses a Docker --mount format string into a bind mount string.
// Input format: type=bind,source=/path,target=/workspace[,consistency=cached][,readonly]
// Output format: source:target[:options]
func parseMountSpec(spec string) string {
	parts := strings.Split(spec, ",")
	var source, target string
	var options []string

	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			// Handle standalone options like "readonly"
			if part == "readonly" || part == "ro" {
				options = append(options, "ro")
			}
			continue
		}
		key, value := kv[0], kv[1]
		switch key {
		case "source", "src":
			source = value
		case "target", "dst", "destination":
			target = value
		case "readonly", "ro":
			if value == "true" || value == "1" {
				options = append(options, "ro")
			}
		case "consistency":
			// macOS consistency option (cached, delegated, consistent)
			options = append(options, value)
		case "type":
			// Skip type=bind, we only support bind mounts here
		}
	}

	if source == "" || target == "" {
		return ""
	}

	result := source + ":" + target
	if len(options) > 0 {
		result += ":" + strings.Join(options, ",")
	}
	return result
}
