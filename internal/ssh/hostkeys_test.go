package ssh

import (
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

func TestEnsureHostKeyHandlesCorruptFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	path, _, err := EnsureHostKey("wk_test")
	if err != nil {
		t.Fatalf("first EnsureHostKey: %v", err)
	}

	// Corrupt the file.
	if err := os.WriteFile(path, []byte("garbage"), 0600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}

	// Should regenerate silently.
	_, signer, err := EnsureHostKey("wk_test")
	if err != nil {
		t.Fatalf("regen EnsureHostKey: %v", err)
	}
	if signer == nil {
		t.Fatal("signer nil after regen")
	}
}

func TestHostKeyDirLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir, err := HostKeyDir()
	if err != nil {
		t.Fatalf("HostKeyDir: %v", err)
	}
	want := filepath.Join(home, ".dcx", "hostkeys")
	if dir != want {
		t.Errorf("HostKeyDir = %q, want %q", dir, want)
	}
}
