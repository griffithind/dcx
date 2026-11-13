package cli

import (
	"fmt"
	"net"
	"strings"

	"github.com/griffithind/dcx/internal/container"
)

// parseHostsSpec parses the user-supplied --hosts value.
//
//	(empty) / "none" / "loopback" -> loopback only
//	"any"                         -> every address
//	"10.0.0.0/24,192.168.1.5"     -> loopback + the listed addresses
//
// Widened access publishes the agent's port on every host interface
// (SSHAccess.BindHost); the agent's allowlist is the gate.
func parseHostsSpec(spec string) (container.SSHAccess, error) {
	trimmed := strings.TrimSpace(spec)
	switch strings.ToLower(trimmed) {
	case "", "none", "loopback":
		return container.SSHAccess{}, nil
	case "any":
		return container.SSHAccess{AllowedCIDRs: []string{"0.0.0.0/0", "::/0"}}, nil
	}

	var out container.SSHAccess
	for _, raw := range strings.Split(trimmed, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Accept bare IPs; promote to /32 or /128.
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip == nil {
				return container.SSHAccess{}, fmt.Errorf("invalid IP address %q", raw)
			}
			if ip.To4() != nil {
				raw = raw + "/32"
			} else {
				raw = raw + "/128"
			}
		}
		if _, _, err := net.ParseCIDR(raw); err != nil {
			return container.SSHAccess{}, fmt.Errorf("invalid CIDR %q: %w", raw, err)
		}
		out.AllowedCIDRs = append(out.AllowedCIDRs, raw)
	}
	if len(out.AllowedCIDRs) == 0 {
		return container.SSHAccess{}, fmt.Errorf("--hosts is empty after parsing %q", spec)
	}
	return out, nil
}
