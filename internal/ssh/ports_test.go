package ssh

import (
	"net"
	"testing"
)

func TestDeterministicPortIsStable(t *testing.T) {
	a := DeterministicPort("wk_abcdef")
	b := DeterministicPort("wk_abcdef")
	if a != b {
		t.Errorf("DeterministicPort is not stable: %d != %d", a, b)
	}
}

func TestDeterministicPortDiffersAcrossWorkspaces(t *testing.T) {
	a := DeterministicPort("wk_one")
	b := DeterministicPort("wk_two")
	// Not a guarantee (FNV can collide) but overwhelmingly likely for two
	// distinct short strings.
	if a == b {
		t.Skipf("rare FNV collision: both workspaces hashed to %d", a)
	}
}

func TestDeterministicPortInRange(t *testing.T) {
	for _, id := range []string{
		"wk_short",
		"wk_a_much_longer_workspace_identifier",
		"xyz",
		"",
		"wk_" + string(make([]byte, 100)),
	} {
		p := DeterministicPort(id)
		if p < sshPortBase || p >= sshPortBase+sshPortRange {
			t.Errorf("DeterministicPort(%q) = %d, outside [%d, %d)", id, p, sshPortBase, sshPortBase+sshPortRange)
		}
	}
}

func TestIsHostPortAvailable(t *testing.T) {
	// Bind an ephemeral port, then ask whether that port is available —
	// it must say false.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	bound := ln.Addr().(*net.TCPAddr).Port
	if IsHostPortAvailable(bound) {
		t.Errorf("IsHostPortAvailable(%d) = true, want false (we own the listener)", bound)
	}
	_ = ln.Close()

	// After closing, the port must be free.
	if !IsHostPortAvailable(bound) {
		t.Errorf("IsHostPortAvailable(%d) after close = false, want true", bound)
	}
}

func TestIsHostPortAvailableForInvalidPorts(t *testing.T) {
	// Binding port 0 produces an ephemeral assignment, not a conflict — so
	// our helper will say "available". That's fine; callers don't pass 0.
	// Port 65536 would fail to bind — confirm we report it unavailable.
	if IsHostPortAvailable(65536) {
		t.Error("IsHostPortAvailable(65536) should return false (out of range)")
	}
}
