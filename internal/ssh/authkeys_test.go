package ssh

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCollectAuthorizedKeys_AlwaysIncludesFallback guards against the
// regression where Claude Desktop / ssh2 and other clients that can only
// present a file-based private key got "All configured authentication
// methods failed" — because when the user had any SSH-agent or on-disk
// key, AuthorizedKeys skipped the dcx fallback entirely.
//
// The fallback MUST appear in authorized_keys regardless of what else is
// present on the host, so clients pointed at ~/.dcx/id_ed25519 can always
// authenticate.
func TestCollectAuthorizedKeys_AlwaysIncludesFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// SSH_AUTH_SOCK intentionally unset — we want to prove the fallback is
	// added even when there is also a disk key.
	t.Setenv("SSH_AUTH_SOCK", "")

	// Pre-populate a user disk key so we exercise the "keys already exist"
	// code path that used to skip the fallback.
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	userPub := []byte("ssh-ed25519 AAAA_USER_KEY_PLACEHOLDER user@host\n")
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519.pub"), userPub, 0644); err != nil {
		t.Fatalf("write user key: %v", err)
	}

	keys, err := AuthorizedKeys()
	if err != nil {
		t.Fatalf("AuthorizedKeys: %v", err)
	}

	if !bytes.Contains(keys, userPub) {
		t.Error("user disk key missing from authorized_keys")
	}

	// The fallback pubkey file must exist and its contents must appear in
	// the output.
	fallbackPub, err := os.ReadFile(filepath.Join(home, ".dcx", "id_ed25519.pub"))
	if err != nil {
		t.Fatalf("fallback pubkey not created: %v", err)
	}
	if !bytes.Contains(keys, fallbackPub) {
		t.Error("fallback pubkey missing from authorized_keys even though it exists on disk")
	}
}

// TestCollectAuthorizedKeys_GeneratesFallbackWhenEmpty covers the
// brand-new-machine path: no ~/.ssh/*.pub, no agent. The function must
// still return a non-empty authorized_keys so `dcx up` produces a usable
// container.
func TestCollectAuthorizedKeys_GeneratesFallbackWhenEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_AUTH_SOCK", "")

	keys, err := AuthorizedKeys()
	if err != nil {
		t.Fatalf("AuthorizedKeys: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("expected fallback to produce a non-empty authorized_keys")
	}

	// The fallback files must exist.
	if _, err := os.Stat(filepath.Join(home, ".dcx", "id_ed25519")); err != nil {
		t.Errorf("fallback private key missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".dcx", "id_ed25519.pub")); err != nil {
		t.Errorf("fallback public key missing: %v", err)
	}
}

// The public half is derived from the private key (F17): a mismatched
// .pub, as two concurrent first runs could leave, is repaired.
func TestFallbackClientKeyRepairsMismatchedPub(t *testing.T) {
	home := t.TempDir()
	first, err := ensureFallbackClientKey(home)
	if err != nil {
		t.Fatal(err)
	}
	pubPath := filepath.Join(home, ".dcx", "id_ed25519.pub")
	if err := os.WriteFile(pubPath, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOtherKeyOtherKeyOtherKeyOtherKeyOtherKeyOt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := ensureFallbackClientKey(home)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(first) {
		t.Errorf("the public key no longer matches the private key:\n%s\n%s", first, again)
	}
	repaired, _ := os.ReadFile(pubPath)
	if strings.TrimSpace(string(repaired)) != strings.TrimSpace(string(first)) {
		t.Error("the .pub file was not rewritten")
	}
	if err := os.Remove(pubPath); err != nil {
		t.Fatal(err)
	}
	if missing, err := ensureFallbackClientKey(home); err != nil || string(missing) != string(first) {
		t.Errorf("a missing .pub must be re-derived, not the pair regenerated: %v", err)
	}
}
