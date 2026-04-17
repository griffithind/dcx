package server

import (
	"net"
	"testing"
)

func TestLoopback(t *testing.T) {
	g := Loopback()

	cases := []struct {
		addr  string
		allow bool
	}{
		{"127.0.0.1:1234", true},
		{"127.1.2.3:1", true},
		{"[::1]:1234", true},
		{"192.168.1.5:22", false},
		{"10.0.0.1:22", false},
		{"8.8.8.8:53", false},
		{"[fe80::1]:22", false},
		{"[2001:db8::1]:22", false},
	}
	for _, c := range cases {
		t.Run(c.addr, func(t *testing.T) {
			addr, err := net.ResolveTCPAddr("tcp", c.addr)
			if err != nil {
				t.Fatalf("ResolveTCPAddr(%q): %v", c.addr, err)
			}
			got := g.Allow(addr)
			if got != c.allow {
				t.Errorf("Allow(%q) = %v, want %v", c.addr, got, c.allow)
			}
		})
	}
}

func TestFromCIDRs(t *testing.T) {
	g, err := FromCIDRs([]string{"10.0.0.0/24", "192.168.1.5", "::1/128"})
	if err != nil {
		t.Fatalf("FromCIDRs: %v", err)
	}

	allowed := []string{
		"10.0.0.1:22", "10.0.0.254:22",
		"192.168.1.5:22",
		"[::1]:22",
	}
	denied := []string{
		"10.0.1.0:22",      // just outside /24
		"192.168.1.6:22",   // not the single host
		"127.0.0.1:22",     // not in any range
		"[fe80::1]:22",
	}

	for _, a := range allowed {
		addr, _ := net.ResolveTCPAddr("tcp", a)
		if !g.Allow(addr) {
			t.Errorf("expected allow %q, was denied", a)
		}
	}
	for _, a := range denied {
		addr, _ := net.ResolveTCPAddr("tcp", a)
		if g.Allow(addr) {
			t.Errorf("expected deny %q, was allowed", a)
		}
	}
}

func TestFromCIDRsMalformed(t *testing.T) {
	cases := []string{
		"not-an-ip",
		"999.999.999.999",
		"10.0.0.0/999",
		"::/999",
	}
	for _, c := range cases {
		if _, err := FromCIDRs([]string{c}); err == nil {
			t.Errorf("FromCIDRs(%q) should have errored", c)
		}
	}
}

func TestFromCIDRsEmpty(t *testing.T) {
	g, err := FromCIDRs(nil)
	if err != nil {
		t.Fatalf("FromCIDRs(nil): %v", err)
	}
	addr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:22")
	if g.Allow(addr) {
		t.Error("empty gate must deny everything, allowed loopback")
	}
}

func TestFromCIDRsSkipsBlank(t *testing.T) {
	g, err := FromCIDRs([]string{"10.0.0.0/24", "", "  ", "::1/128"})
	if err != nil {
		t.Fatalf("FromCIDRs: %v", err)
	}
	// Blank entries should be silently dropped; verify via observed Allow
	// behavior for addresses inside each configured range.
	a, _ := net.ResolveTCPAddr("tcp", "10.0.0.1:22")
	b, _ := net.ResolveTCPAddr("tcp", "[::1]:22")
	if !g.Allow(a) || !g.Allow(b) {
		t.Error("both configured CIDRs should still be allowed")
	}
}

func TestExtend(t *testing.T) {
	base := Loopback()
	g, err := base.Extend([]string{"172.17.0.1/32"})
	if err != nil {
		t.Fatalf("Extend: %v", err)
	}

	addr1, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:22")
	addr2, _ := net.ResolveTCPAddr("tcp", "172.17.0.1:22")
	addr3, _ := net.ResolveTCPAddr("tcp", "172.17.0.2:22")

	if !g.Allow(addr1) {
		t.Error("extended gate should still allow loopback")
	}
	if !g.Allow(addr2) {
		t.Error("extended gate should allow new CIDR")
	}
	if g.Allow(addr3) {
		t.Error("extended gate should not allow address outside any CIDR")
	}
}

func TestAllowNilSafety(t *testing.T) {
	var g *Gate
	if g.Allow(nil) {
		t.Error("nil Gate.Allow(nil) should return false")
	}
}

func TestAllowNonIPAddr(t *testing.T) {
	g := Loopback()
	// UnixAddr has no IP to parse, so should be rejected.
	addr := &net.UnixAddr{Name: "/tmp/sock", Net: "unix"}
	if g.Allow(addr) {
		t.Error("Unix addr should not satisfy an IP gate")
	}
}

