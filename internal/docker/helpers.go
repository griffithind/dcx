package docker

import (
	"fmt"
	"strings"

	"github.com/docker/go-connections/nat"
	"github.com/griffithind/dcx/internal/parse"
)

// parsePortBindings parses port specifications into exposed ports and port bindings.
// Supports formats: "8080", "8080:80", "127.0.0.1:8080:80", "8080/udp"
func parsePortBindings(ports []string) (nat.PortSet, nat.PortMap) {
	bindings := parse.ParsePortBindings(ports)

	exposedPorts := make(nat.PortSet)
	portBindings := make(nat.PortMap)

	for _, pb := range bindings {
		natPort := nat.Port(fmt.Sprintf("%s/%s", pb.ContainerPort, pb.Protocol))
		exposedPorts[natPort] = struct{}{}
		portBindings[natPort] = []nat.PortBinding{
			{
				HostIP:   pb.HostIP,
				HostPort: pb.HostPort,
			},
		}
	}

	return exposedPorts, portBindings
}

// parseMountSpec parses a Docker --mount format string into a bind mount string.
// Input format: type=bind,source=/path,target=/workspace[,consistency=cached][,readonly]
// Output format: source:target[:options]
func parseMountSpec(spec string) string {
	m := parse.ParseMount(spec)
	if m == nil {
		return ""
	}

	result := m.Source + ":" + m.Target
	var opts []string
	if m.ReadOnly {
		opts = append(opts, "ro")
	}
	if m.Consistency != "" {
		opts = append(opts, m.Consistency)
	}
	if len(opts) > 0 {
		result += ":" + strings.Join(opts, ",")
	}
	return result
}
