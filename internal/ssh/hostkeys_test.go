package ssh

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureHostKeyGenerates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, signer, err := EnsureHostKey("wk_test")
	if err != nil {
		t.Fatalf("EnsureHostKey: %v", err)
	}
	if signer == nil {
		t.Fatal("signer is nil")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat key path: %v", err)
	}
}

func TestEnsureHostKeyIdempotent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path1, s1, err := EnsureHostKey("wk_test")
	if err != nil {
		t.Fatalf("first EnsureHostKey: %v", err)
	}
	fp1 := Fingerprint(s1)

	path2, s2, err := EnsureHostKey("wk_test")
	if err != nil {
		t.Fatalf("second EnsureHostKey: %v", err)
	}
	fp2 := Fingerprint(s2)

	if path1 != path2 {
		t.Errorf("path changed between calls: %q vs %q", path1, path2)
	}
	if fp1 != fp2 {
		t.Errorf("fingerprint changed between calls: %q vs %q", fp1, fp2)
	}
}

func TestEnsureHostKeyPermissions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, _, err := EnsureHostKey("wk_test")
	if err != nil {
		t.Fatalf("EnsureHostKey: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0600 {
		t.Errorf("expected mode 0600, got %v", mode)
	}
}

func TestEnsureHostKeyRepairsDriftedPerms(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, _, err := EnsureHostKey("wk_test")
	if err != nil {
		t.Fatalf("first EnsureHostKey: %v", err)
	}

	// Simulate perm drift (e.g. restored from backup with wrong umask).
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("chmod 0644: %v", err)
	}

	if _, _, err := EnsureHostKey("wk_test"); err != nil {
		t.Fatalf("second EnsureHostKey: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0600 {
		t.Errorf("expected perms repaired to 0600, got %v", mode)
	}
}

func TestEnsureHostKeyRefusesCorruptFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, _, err := EnsureHostKey("wk_test")
	if err != nil {
		t.Fatalf("first EnsureHostKey: %v", err)
	}
	if err := os.WriteFile(path, []byte("garbage"), 0600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	// No silent regeneration: that would break every pinned client.
	if _, _, err := EnsureHostKey("wk_test"); err == nil {
		t.Fatal("EnsureHostKey must refuse a corrupt key")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "garbage" {
		t.Error("corrupt key was overwritten")
	}
}

func TestLoadHostKeyNeverCreates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, _, err := LoadHostKey("wk_missing")
	if !errors.Is(err, ErrNoHostKey) {
		t.Fatalf("want ErrNoHostKey, got %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("LoadHostKey created a key")
	}
	if _, _, err := LoadHostKey(""); err == nil {
		t.Error("LoadHostKey without a workspace ID must fail")
	}

	created, _, err := EnsureHostKey("wk_missing")
	if err != nil {
		t.Fatal(err)
	}
	loaded, signer, err := LoadHostKey("wk_missing")
	if err != nil || signer == nil || loaded != created {
		t.Fatalf("LoadHostKey after EnsureHostKey: %v", err)
	}
	if err := RemoveHostKey("wk_missing"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadHostKey("wk_missing"); !errors.Is(err, ErrNoHostKey) {
		t.Errorf("key still present after RemoveHostKey: %v", err)
	}
}

// The host key lives in the workspace's folder; one an older dcx kept in
// hostkeys/<id>.key moves there on first use, so pins of it stay valid.
func TestHostKeyLayoutAndMigration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("DCX_HOME", "")

	path, err := HostKeyPath("ws1")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".dcx", "workspaces", "ws1", "host_key"); path != want {
		t.Errorf("HostKeyPath = %q, want %q", path, want)
	}

	legacy := filepath.Join(home, ".dcx", "hostkeys", "ws2.key")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	_, old, err := EnsureHostKey("ws2")
	if err != nil {
		t.Fatal(err)
	}
	moved, _ := HostKeyPath("ws2")
	if err := os.Rename(moved, legacy); err != nil { // as an older dcx left it
		t.Fatal(err)
	}
	_, signer, err := LoadHostKey("ws2")
	if err != nil {
		t.Fatalf("LoadHostKey after migration: %v", err)
	}
	if Fingerprint(signer) != Fingerprint(old) {
		t.Error("the migrated key is the same key")
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("the legacy key was moved, not copied")
	}
}
