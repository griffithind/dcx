package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func generateTestPub(t *testing.T) gossh.PublicKey {
	t.Helper()
	pubEd, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pub, err := gossh.NewPublicKey(pubEd)
	if err != nil {
		t.Fatalf("NewPublicKey: %v", err)
	}
	return pub
}

func TestPinAndHasHost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	has, err := HasHost("wk_test")
	if err != nil {
		t.Fatalf("HasHost (empty): %v", err)
	}
	if has {
		t.Error("empty known_hosts should return HasHost=false")
	}

	pub := generateTestPub(t)
	if err := PinHostKey("wk_test", pub); err != nil {
		t.Fatalf("PinHostKey: %v", err)
	}

	has, err = HasHost("wk_test")
	if err != nil {
		t.Fatalf("HasHost: %v", err)
	}
	if !has {
		t.Error("after PinHostKey, HasHost should return true")
	}
}

func TestPinHostKeyReplacesExisting(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	pub1 := generateTestPub(t)
	pub2 := generateTestPub(t)

	if err := PinHostKey("wk_test", pub1); err != nil {
		t.Fatalf("pin 1: %v", err)
	}
	if err := PinHostKey("wk_test", pub2); err != nil {
		t.Fatalf("pin 2: %v", err)
	}

	path, _ := KnownHostsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}

	// Count lines matching the alias — should be exactly 1.
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if lineMatchesAlias(line, "dcx-wk_test") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 entry after replace, got %d", count)
	}
}

func TestRemoveHost(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	pub := generateTestPub(t)
	if err := PinHostKey("wk_a", pub); err != nil {
		t.Fatalf("pin a: %v", err)
	}
	if err := PinHostKey("wk_b", pub); err != nil {
		t.Fatalf("pin b: %v", err)
	}

	if err := RemoveHost("wk_a"); err != nil {
		t.Fatalf("remove a: %v", err)
	}

	hasA, _ := HasHost("wk_a")
	hasB, _ := HasHost("wk_b")
	if hasA {
		t.Error("wk_a should be removed")
	}
	if !hasB {
		t.Error("wk_b should still be present")
	}
}

func TestRemoveHostMissingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	// No known_hosts file exists — should succeed silently.
	if err := RemoveHost("wk_nonexistent"); err != nil {
		t.Errorf("RemoveHost on missing file should not error, got %v", err)
	}
}

func TestHostKeyAlias(t *testing.T) {
	if got := HostKeyAlias("abc123"); got != "dcx-abc123" {
		t.Errorf("HostKeyAlias = %q, want %q", got, "dcx-abc123")
	}
}

func TestKnownHostsFilePermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	pub := generateTestPub(t)
	if err := PinHostKey("wk_test", pub); err != nil {
		t.Fatalf("PinHostKey: %v", err)
	}

	path, _ := KnownHostsPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected 0600, got %v", info.Mode().Perm())
	}
}

func TestLineMatchesAlias(t *testing.T) {
	cases := []struct {
		line  string
		alias string
		want  bool
	}{
		{"dcx-wk_test ssh-ed25519 AAAA", "dcx-wk_test", true},
		{"dcx-wk_other ssh-ed25519 AAAA", "dcx-wk_test", false},
		{"dcx-wk_test,alt ssh-ed25519 AAAA", "dcx-wk_test", true},
		{"# comment", "dcx-wk_test", false},
		{"", "dcx-wk_test", false},
		{"  ", "dcx-wk_test", false},
		{"dcx-wk_test", "dcx-wk_test", false}, // too few fields
	}
	for _, c := range cases {
		got := lineMatchesAlias(c.line, c.alias)
		if got != c.want {
			t.Errorf("lineMatchesAlias(%q, %q) = %v, want %v", c.line, c.alias, got, c.want)
		}
	}
}
