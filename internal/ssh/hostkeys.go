// Package ssh provides host-side SSH key management for dcx.
//
// hostkeys.go manages per-workspace SSH host keys stored under
// ~/.dcx/hostkeys/<workspaceID>.key. The same key is bind-mounted into the
// container as /run/secrets/dcx/ssh_host_ed25519_key so the agent presents
// a stable fingerprint across container recreation.
package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	gossh "golang.org/x/crypto/ssh"
)

// HostKeyDir returns the directory where per-workspace host keys live.
// Callers should not assume the directory exists; EnsureHostKey creates it.
func HostKeyDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("home dir: %w", err)
	}
	return filepath.Join(home, ".dcx", "hostkeys"), nil
}

// HostKeyPath returns the absolute path to a workspace's host key file.
func HostKeyPath(workspaceID string) (string, error) {
	dir, err := HostKeyDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, workspaceID+".key"), nil
}

// EnsureHostKey loads the existing host key for the workspace, or generates
// a new ed25519 key and persists it. Returns the filesystem path and the
// parsed signer.
//
// The key file is written with mode 0600. If an existing file has loose
// perms, EnsureHostKey repairs them to 0600 — drift can happen when users
// back up and restore dotfiles without preserving modes.
func EnsureHostKey(workspaceID string) (path string, signer gossh.Signer, err error) {
	path, err = HostKeyPath(workspaceID)
	if err != nil {
		return "", nil, err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, fmt.Errorf("create host key dir: %w", err)
	}

	// Try to load an existing key.
	if data, rerr := os.ReadFile(path); rerr == nil {
		// Repair perms if they drifted.
		_ = os.Chmod(path, 0600)
		s, perr := gossh.ParsePrivateKey(data)
		if perr == nil {
			return path, s, nil
		}
		// Corrupt or unrecognized key — regenerate.
	}

	// Generate a new ed25519 key.
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, fmt.Errorf("generate ed25519: %w", err)
	}

	pemBlock, err := gossh.MarshalPrivateKey(priv, "dcx-agent host key")
	if err != nil {
		return "", nil, fmt.Errorf("marshal private key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(pemBlock)

	// Write atomically: write to .tmp, rename into place.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, pemBytes, 0600); err != nil {
		return "", nil, fmt.Errorf("write tmp host key: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", nil, fmt.Errorf("rename host key: %w", err)
	}

	s, err := gossh.ParsePrivateKey(pemBytes)
	if err != nil {
		return "", nil, fmt.Errorf("parse generated key: %w", err)
	}
	return path, s, nil
}

// Fingerprint returns the SHA256 fingerprint of the signer's public key, in
// OpenSSH "SHA256:…" form. Used for display and audit logging.
func Fingerprint(signer gossh.Signer) string {
	return gossh.FingerprintSHA256(signer.PublicKey())
}
