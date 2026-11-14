package devcontainer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
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

// parsePorts parses port entries, reporting entries it cannot publish.
func (b *Builder) parsePorts(field string, ports PortList) []PortForward {
	var out []PortForward
	for _, p := range ports {
		spec := p.Spec
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

// Port is one forwardPorts/appPort entry: a port number or a port spec
// ("8080:80", "127.0.0.1::90", "db:5432"; see ParsePortSpec).
type Port struct {
	Spec string
	// number and bare record how the entry was written (a JSON number; a
	// value rather than an array) so it marshals back unchanged: the
	// config hash covers forwardPorts and appPort.
	number bool
	bare   bool
}

// PortList is forwardPorts or appPort: a number, a string or an array of
// them.
type PortList []Port

// UnmarshalJSON accepts a number, a string or an array of either.
func (l *PortList) UnmarshalJSON(data []byte) error {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if items, ok := raw.([]interface{}); ok {
		out := make(PortList, 0, len(items))
		for _, item := range items {
			p, err := portFromValue(item)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		*l = out
		return nil
	}
	p, err := portFromValue(raw)
	if err != nil {
		return err
	}
	p.bare = true
	*l = PortList{p}
	return nil
}

func portFromValue(v interface{}) (Port, error) {
	switch t := v.(type) {
	case float64:
		return Port{Spec: strconv.Itoa(int(t)), number: true}, nil
	case string:
		return Port{Spec: t}, nil
	}
	return Port{}, fmt.Errorf("port: expected a number or a string, got %v", v)
}

// MarshalJSON writes the list in the form it was read in.
func (l PortList) MarshalJSON() ([]byte, error) {
	if len(l) == 1 && l[0].bare {
		return l[0].MarshalJSON()
	}
	return json.Marshal([]Port(l))
}

// MarshalJSON writes a number entry as a number and a spec as a string.
func (p Port) MarshalJSON() ([]byte, error) {
	if p.number {
		if n, err := strconv.Atoi(p.Spec); err == nil {
			return json.Marshal(n)
		}
	}
	return json.Marshal(p.Spec)
}

// union appends the entries of b not already in l (by spec).
func (l PortList) union(b PortList) PortList {
	for _, p := range b {
		if !slices.ContainsFunc(l, func(q Port) bool { return q.Spec == p.Spec }) {
			p.bare = false
			l = append(l, p)
		}
	}
	return l
}
