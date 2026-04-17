package cli

import "testing"

func TestParseHostsSpecDefaults(t *testing.T) {
	cases := []string{"", "  ", "none", "NONE", "loopback"}
	for _, spec := range cases {
		out, err := parseHostsSpec(spec)
		if err != nil {
			t.Fatalf("parseHostsSpec(%q): %v", spec, err)
		}
		if out.BindHost != "127.0.0.1" {
			t.Errorf("%q: expected loopback bind, got %q", spec, out.BindHost)
		}
		if len(out.CIDRs) != 0 {
			t.Errorf("%q: expected empty CIDRs, got %v", spec, out.CIDRs)
		}
	}
}

func TestParseHostsSpecAny(t *testing.T) {
	out, err := parseHostsSpec("any")
	if err != nil {
		t.Fatalf("parseHostsSpec: %v", err)
	}
	if out.BindHost != "0.0.0.0" {
		t.Errorf("expected 0.0.0.0, got %q", out.BindHost)
	}
	if len(out.CIDRs) < 2 {
		t.Errorf("expected both v4 and v6 wildcards, got %v", out.CIDRs)
	}
}

func TestParseHostsSpecCIDR(t *testing.T) {
	out, err := parseHostsSpec("10.0.0.0/24,192.168.1.5")
	if err != nil {
		t.Fatalf("parseHostsSpec: %v", err)
	}
	if out.BindHost != "0.0.0.0" {
		t.Errorf("expected 0.0.0.0 bind, got %q", out.BindHost)
	}
	want := []string{"10.0.0.0/24", "192.168.1.5/32"}
	if len(out.CIDRs) != len(want) {
		t.Fatalf("expected %v, got %v", want, out.CIDRs)
	}
	for i, w := range want {
		if out.CIDRs[i] != w {
			t.Errorf("CIDRs[%d] = %q, want %q", i, out.CIDRs[i], w)
		}
	}
}

func TestParseHostsSpecIPv6(t *testing.T) {
	out, err := parseHostsSpec("::1")
	if err != nil {
		t.Fatalf("parseHostsSpec: %v", err)
	}
	if len(out.CIDRs) != 1 || out.CIDRs[0] != "::1/128" {
		t.Errorf("expected [::1/128], got %v", out.CIDRs)
	}
}

func TestParseHostsSpecErrors(t *testing.T) {
	cases := []string{
		"not-an-ip",
		"999.999.999.999",
		"10.0.0.0/999",
		",,",
	}
	for _, c := range cases {
		if _, err := parseHostsSpec(c); err == nil {
			t.Errorf("parseHostsSpec(%q) should have errored", c)
		}
	}
}
