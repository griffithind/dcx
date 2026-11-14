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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/dcxhome"
	gossh "golang.org/x/crypto/ssh"
)

// HostKeyDir returns the directory where per-workspace host keys live.
// Callers should not assume the directory exists; EnsureHostKey creates it.
func HostKeyDir() (string, error) {
	return dcxhome.Path("hostkeys")
}

// HostKeyPath returns the absolute path to a workspace's host key file.
func HostKeyPath(workspaceID string) (string, error) {
	dir, err := HostKeyDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, workspaceID+".key"), nil
}

// ErrNoHostKey means the workspace has no host key yet (`dcx up` creates it).
var ErrNoHostKey = errors.New("no SSH host key for this workspace; run `dcx up`")

// LoadHostKey loads the workspace's existing host key. It never creates or
// replaces one: clients pin exactly the key `dcx up` put in the container,
// so a missing key is ErrNoHostKey and a corrupt one is an error.
func LoadHostKey(workspaceID string) (path string, signer gossh.Signer, err error) {
	if workspaceID == "" {
		return "", nil, fmt.Errorf("workspace ID is required to load its host key")
	}
	path, err = HostKeyPath(workspaceID)
	if err != nil {
		return "", nil, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return path, nil, ErrNoHostKey
	}
	if err != nil {
		return path, nil, fmt.Errorf("read host key: %w", err)
	}
	// Repair perms if they drifted (dotfile restores often lose modes).
	_ = os.Chmod(path, 0600)
	signer, err = gossh.ParsePrivateKey(data)
	if err != nil {
		return path, nil, fmt.Errorf("host key %s is unreadable (remove it and run `dcx up --recreate`): %w", path, err)
	}
	return path, signer, nil
}

// EnsureHostKey returns the workspace's host key, generating and persisting
// a new ed25519 key only when none exists. Only `dcx up` calls it, before
// the key is written into the container. A corrupt key is an error rather
// than silently replaced, which would break every pinned client.
func EnsureHostKey(workspaceID string) (path string, signer gossh.Signer, err error) {
	path, signer, err = LoadHostKey(workspaceID)
	if !errors.Is(err, ErrNoHostKey) {
		return path, signer, err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", nil, fmt.Errorf("create host key dir: %w", err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, fmt.Errorf("generate ed25519: %w", err)
	}
	pemBlock, err := gossh.MarshalPrivateKey(priv, "dcx-agent host key")
	if err != nil {
		return "", nil, fmt.Errorf("marshal private key: %w", err)
	}
	pemBytes := pem.EncodeToMemory(pemBlock)

	// Write to a unique temp file and link it into place: the link fails
	// if another process created the key meanwhile, and then that key
	// wins (replacing it would break the other process's pins).
	tmp, err := writeTemp(path, pemBytes, 0600)
	if err != nil {
		return "", nil, fmt.Errorf("write host key: %w", err)
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadHostKey(workspaceID)
		}
		return "", nil, fmt.Errorf("install host key: %w", err)
	}
	signer, err = gossh.ParsePrivateKey(pemBytes)
	if err != nil {
		return "", nil, fmt.Errorf("parse generated key: %w", err)
	}
	return path, signer, nil
}

// RemoveHostKey deletes the workspace's host key (on `dcx down`), so keys of
// removed environments do not accumulate under ~/.dcx/hostkeys.
func RemoveHostKey(workspaceID string) error {
	if workspaceID == "" {
		return nil
	}
	path, err := HostKeyPath(workspaceID)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// Fingerprint returns the SHA256 fingerprint of the signer's public key, in
// OpenSSH "SHA256:…" form. Used for display and audit logging.
func Fingerprint(signer gossh.Signer) string {
	return gossh.FingerprintSHA256(signer.PublicKey())
}
