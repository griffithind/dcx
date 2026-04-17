// Package server — the netgate enforces which source IPs may complete an
// SSH handshake. It is wired into gliderlabs/ssh's ConnCallback, which runs
// before any SSH bytes are exchanged. A nil return from the callback closes
// the connection.
//
// The gate is the load-bearing pre-handshake filter. Even though Docker's
// -p <bind>:<host>:<container> restricts which host interface accepts
// packets, the agent may bind 0.0.0.0 inside the container (WSL2 NAT) — in
// that case the gate is the only layer enforcing "loopback-only by default."
package server

import (
	"fmt"
	"net"
	"strings"
)

// Gate is an IP allowlist. It answers the question: should this source
// address be allowed through the handshake?
type Gate struct {
	nets []*net.IPNet
}

// Loopback returns a Gate that permits only 127.0.0.0/8 and ::1/128.
func Loopback() *Gate {
	_, v4, _ := net.ParseCIDR("127.0.0.0/8")
	_, v6, _ := net.ParseCIDR("::1/128")
	return &Gate{nets: []*net.IPNet{v4, v6}}
}

// FromCIDRs parses a list of CIDR strings (or bare IPs, which are treated as
// /32 or /128) into a Gate. Returns an error if any entry is malformed.
//
// An empty list returns a Gate that rejects all connections. Callers that
// want "loopback-only" should either combine with [Loopback] or include
// "127.0.0.0/8" and "::1/128" explicitly.
func FromCIDRs(cidrs []string) (*Gate, error) {
	g := &Gate{nets: make([]*net.IPNet, 0, len(cidrs))}
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		// If no / present, treat as a bare IP (/32 for v4, /128 for v6).
		if !strings.Contains(raw, "/") {
			ip := net.ParseIP(raw)
			if ip == nil {
				return nil, fmt.Errorf("invalid IP address %q", raw)
			}
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			mask := net.CIDRMask(bits, bits)
			if ip.To4() != nil {
				ip = ip.To4()
			}
			g.nets = append(g.nets, &net.IPNet{IP: ip, Mask: mask})
			continue
		}

		_, n, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", raw, err)
		}
		g.nets = append(g.nets, n)
	}
	return g, nil
}

// Extend returns a new Gate that accepts everything this gate accepts plus
// the additional CIDRs. It is used when the auto-detected WSL2 Hyper-V
// gateway needs to be added to a loopback-only gate at runtime.
func (g *Gate) Extend(cidrs []string) (*Gate, error) {
	extra, err := FromCIDRs(cidrs)
	if err != nil {
		return nil, err
	}
	out := &Gate{nets: make([]*net.IPNet, 0, len(g.nets)+len(extra.nets))}
	out.nets = append(out.nets, g.nets...)
	out.nets = append(out.nets, extra.nets...)
	return out, nil
}

// Allow reports whether the given address is permitted.
// Returns false for nil addresses, non-IP addresses, unparseable host
// components, and IPs outside the configured CIDRs.
func (g *Gate) Allow(addr net.Addr) bool {
	if g == nil || addr == nil {
		return false
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		// Some Addr implementations don't have ports; try the raw string.
		host = addr.String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range g.nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

