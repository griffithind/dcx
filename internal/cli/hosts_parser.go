package cli

import (
	"fmt"
	"net"
	"strings"
)

// parsedHosts captures the result of parsing --hosts into the pieces dcx
// needs to wire both sides of the network gate (Docker -p + agent
// ConnCallback).
type parsedHosts struct {
	BindHost string   // "127.0.0.1" (default/none), or "0.0.0.0" (any extra/any)
	CIDRs    []string // CIDRs beyond loopback that the ConnCallback accepts.
}

// parseHostsSpec parses the user-supplied --hosts value.
//
//	(empty) / "none" / "loopback" -> loopback-only (127.0.0.1, gate=loopback)
//	"any"                         -> bind 0.0.0.0, gate accepts everyone
//	"10.0.0.0/24,192.168.1.5"     -> bind 0.0.0.0, gate accepts loopback + listed
//
// An empty gate is never allowed — either loopback (default) or the listed
// CIDRs are appended to the loopback baseline.
func parseHostsSpec(spec string) (parsedHosts, error) {
	trimmed := strings.TrimSpace(spec)
	switch strings.ToLower(trimmed) {
	case "", "none", "loopback":
		return parsedHosts{BindHost: "127.0.0.1"}, nil
	case "any":
		return parsedHosts{
			BindHost: "0.0.0.0",
			CIDRs:    []string{"0.0.0.0/0", "::/0"},
		}, nil
	}

	out := parsedHosts{BindHost: "0.0.0.0"}
	for _, raw := range strings.Split(trimmed, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		// Accept bare IPs; promote to /32 or /128.
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip == nil {
				return parsedHosts{}, fmt.Errorf("invalid IP address %q", raw)
			}
			if ip.To4() != nil {
				raw = raw + "/32"
			} else {
				raw = raw + "/128"
			}
		}
		if _, _, err := net.ParseCIDR(raw); err != nil {
			return parsedHosts{}, fmt.Errorf("invalid CIDR %q: %w", raw, err)
		}
		out.CIDRs = append(out.CIDRs, raw)
	}
	if len(out.CIDRs) == 0 {
		return parsedHosts{}, fmt.Errorf("--hosts is empty after parsing %q", spec)
	}
	return out, nil
}
