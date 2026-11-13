package devcontainer

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// errRemoteHostPort marks a forwardPorts entry of the form "host:port" that
// names another host (e.g. a compose service). Such ports are forwarded by
// editors over the network; dcx cannot publish them from the dev container.
var errRemoteHostPort = errors.New("refers to another host")

// ParsePortSpec parses a port specification. It is the only port parser in
// dcx and accepts the forms used by forwardPorts, appPort and docker -p:
//
//	"3000"                  container port 3000 on host port 3000
//	"8080:80"               host port 8080 -> container port 80
//	"127.0.0.1:9000:90"     bound to 127.0.0.1
//	"127.0.0.1::90"         bound to 127.0.0.1, Docker picks the host port
//	any of the above + "/udp" or "/tcp"
//
// "db:5432" (a non-numeric, non-IP host) returns errRemoteHostPort.
func ParsePortSpec(spec string) (PortForward, error) {
	var pf PortForward
	s := strings.TrimSpace(spec)
	if s == "" {
		return pf, fmt.Errorf("empty port")
	}
	if base, proto, ok := strings.Cut(s, "/"); ok {
		if proto != "tcp" && proto != "udp" && proto != "sctp" {
			return pf, fmt.Errorf("invalid port %q: unknown protocol %q", spec, proto)
		}
		pf.Protocol = proto
		s = base
	}

	port := func(p string) (int, error) {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return 0, fmt.Errorf("invalid port %q", spec)
		}
		return n, nil
	}

	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		n, err := port(parts[0])
		if err != nil {
			return pf, err
		}
		pf.HostPort, pf.ContainerPort = n, n
	case 2:
		if _, err := strconv.Atoi(parts[0]); err != nil && net.ParseIP(parts[0]) == nil {
			return pf, fmt.Errorf("port %q %w", spec, errRemoteHostPort)
		}
		hp, err := port(parts[0])
		if err != nil {
			return pf, err
		}
		cp, err := port(parts[1])
		if err != nil {
			return pf, err
		}
		pf.HostPort, pf.ContainerPort = hp, cp
	case 3:
		if net.ParseIP(parts[0]) == nil && parts[0] != "localhost" {
			return pf, fmt.Errorf("invalid port %q: %q is not an IP address", spec, parts[0])
		}
		pf.Host = parts[0]
		cp, err := port(parts[2])
		if err != nil {
			return pf, err
		}
		pf.ContainerPort = cp
		if parts[1] == "" {
			pf.EphemeralHostPort = true
		} else if pf.HostPort, err = port(parts[1]); err != nil {
			return pf, err
		}
	default:
		return pf, fmt.Errorf("invalid port %q", spec)
	}
	return pf, nil
}

// DockerSpec renders the binding in docker -p / compose "ports" syntax.
func (p PortForward) DockerSpec() string {
	var s string
	switch {
	case p.EphemeralHostPort && p.Host != "":
		s = fmt.Sprintf("%s::%d", p.Host, p.ContainerPort)
	case p.EphemeralHostPort:
		s = fmt.Sprintf("::%d", p.ContainerPort)
	default:
		hostPort := p.HostPort
		if hostPort == 0 {
			hostPort = p.ContainerPort
		}
		if p.Host != "" {
			s = fmt.Sprintf("%s:%d:%d", p.Host, hostPort, p.ContainerPort)
		} else {
			s = fmt.Sprintf("%d:%d", hostPort, p.ContainerPort)
		}
	}
	if p.Protocol != "" && p.Protocol != "tcp" {
		s += "/" + p.Protocol
	}
	return s
}

// portValues flattens a JSON port value (number, string or array of them).
func portValues(v interface{}) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case float64:
		return []string{strconv.Itoa(int(t))}
	case int:
		return []string{strconv.Itoa(t)}
	case string:
		return []string{t}
	case []interface{}:
		var out []string
		for _, item := range t {
			out = append(out, portValues(item)...)
		}
		return out
	default:
		return []string{fmt.Sprint(t)}
	}
}

// parsePorts parses JSON port values, reporting entries it cannot publish.
func (b *Builder) parsePorts(field string, v interface{}) []PortForward {
	var out []PortForward
	for _, spec := range portValues(v) {
		pf, err := ParsePortSpec(spec)
		if err != nil {
			if errors.Is(err, errRemoteHostPort) {
				b.report.Infof("%s: skipping %q (it %v; editors forward it over the network)", field, spec, errRemoteHostPort)
			} else {
				b.report.Warnf("%s: ignoring %v", field, err)
			}
			continue
		}
		out = append(out, pf)
	}
	return out
}
