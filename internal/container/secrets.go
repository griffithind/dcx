package container

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"

	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/secrets"
)

// dcxSecretsSubdir is the namespaced subdirectory under SecretsDir where
// dcx-managed secrets live. Keeps them from colliding with user secret
// names like "API_KEY" that are stored flat under SecretsDir.
const dcxSecretsSubdir = "dcx"

// DCXSecretPath returns the absolute in-container path for a dcx-managed
// secret by name. Example: DCXSecretPath("authorized_keys") →
// "/run/secrets/dcx/authorized_keys".
func DCXSecretPath(name string) string {
	return filepath.Join(common.SecretsDir, dcxSecretsSubdir, name)
}

// DCXSecretsDir returns the absolute in-container path of the dcx-managed
// secrets directory ("/run/secrets/dcx").
func DCXSecretsDir() string {
	return filepath.Join(common.SecretsDir, dcxSecretsSubdir)
}

// MountSecretsToContainer copies secrets to /run/secrets in the specified container.
// Secrets are written as files with mode 0400 (read-only by owner).
// The owner parameter specifies the user who should own the secrets (e.g., "node", "root").
// This is a standalone function that can be called without a runtime instance.
func MountSecretsToContainer(ctx context.Context, containerName string, secretList []secrets.Secret, owner string) error {
	if len(secretList) == 0 {
		return nil
	}

	if containerName == "" {
		return fmt.Errorf("container name not set")
	}

	// Default to root if no owner specified
	if owner == "" {
		owner = "root"
	}

	docker := MustDocker()

	// Create /run/secrets directory if it doesn't exist
	if err := docker.MkdirInContainer(ctx, containerName, common.SecretsDir, "root"); err != nil {
		return fmt.Errorf("failed to create secrets directory: %w", err)
	}

	// Set permissions (755 for directory)
	if err := docker.ChmodInContainer(ctx, containerName, common.SecretsDir, "755", "root"); err != nil {
		return fmt.Errorf("failed to set directory permissions: %w", err)
	}

	// Write each secret to the container
	for _, secret := range secretList {
		if err := writeSecretToContainer(ctx, docker, containerName, secret, owner); err != nil {
			return fmt.Errorf("failed to write secret %q: %w", secret.Name, err)
		}
	}

	return nil
}

// writeSecretToContainer writes a secret to the container's /run/secrets.
// Uses docker exec to write directly (docker cp doesn't work with tmpfs mounts).
func writeSecretToContainer(ctx context.Context, docker *Docker, containerName string, secret secrets.Secret, owner string) error {
	destPath := filepath.Join(common.SecretsDir, secret.Name)

	// Write secret content directly to container using docker exec
	// (docker cp doesn't work with tmpfs mounts)
	if err := docker.WriteFileInContainer(ctx, containerName, destPath, secret.Value, "root"); err != nil {
		return err
	}

	// Set ownership to the specified user
	if err := docker.ChownInContainer(ctx, containerName, destPath, owner); err != nil {
		return err
	}

	// Set permissions (400 for secret files - read-only by owner)
	if err := docker.ChmodInContainer(ctx, containerName, destPath, "400", "root"); err != nil {
		return err
	}

	return nil
}

// DCXSecret is a dcx-managed tmpfs secret written into
// /run/secrets/dcx/<Name>. Used for the SSH authorized_keys list and the
// persistent host key. Unlike user secrets, these are readable by the
// SSH daemon (owned by root, mode 0444 for authorized_keys, 0400 for
// the host key).
type DCXSecret struct {
	Name    string // filename under /run/secrets/dcx/
	Value   []byte
	Mode    string // e.g. "0444" or "0400"
	Owner   string // "root" unless the file must be readable by the remote user
}

// MountDCXSecrets writes a set of dcx-managed secrets into the container's
// /run/secrets/dcx/ directory. Creates the subdirectory if necessary.
//
// Unlike MountSecretsToContainer (which handles user-defined secrets at
// /run/secrets/<name>), this function targets the dcx/ namespace so the
// two categories cannot collide.
func MountDCXSecrets(ctx context.Context, containerName string, mgmt []DCXSecret) error {
	if len(mgmt) == 0 {
		return nil
	}
	if containerName == "" {
		return fmt.Errorf("container name not set")
	}

	docker := MustDocker()

	// Ensure /run/secrets and /run/secrets/dcx both exist.
	if err := docker.MkdirInContainer(ctx, containerName, common.SecretsDir, "root"); err != nil {
		return fmt.Errorf("failed to create secrets directory: %w", err)
	}
	if err := docker.ChmodInContainer(ctx, containerName, common.SecretsDir, "755", "root"); err != nil {
		return fmt.Errorf("failed to set secrets directory permissions: %w", err)
	}

	dcxDir := DCXSecretsDir()
	if err := docker.MkdirInContainer(ctx, containerName, dcxDir, "root"); err != nil {
		return fmt.Errorf("failed to create dcx secrets directory: %w", err)
	}
	if err := docker.ChmodInContainer(ctx, containerName, dcxDir, "755", "root"); err != nil {
		return fmt.Errorf("failed to set dcx secrets directory permissions: %w", err)
	}

	for _, s := range mgmt {
		owner := s.Owner
		if owner == "" {
			owner = "root"
		}
		mode := s.Mode
		if mode == "" {
			mode = "0400"
		}
		destPath := DCXSecretPath(s.Name)

		if err := docker.WriteFileInContainer(ctx, containerName, destPath, s.Value, "root"); err != nil {
			return fmt.Errorf("write dcx secret %q: %w", s.Name, err)
		}
		if err := docker.ChownInContainer(ctx, containerName, destPath, owner); err != nil {
			return fmt.Errorf("chown dcx secret %q: %w", s.Name, err)
		}
		if err := docker.ChmodInContainer(ctx, containerName, destPath, mode, "root"); err != nil {
			return fmt.Errorf("chmod dcx secret %q: %w", s.Name, err)
		}
	}

	return nil
}

// SHA256Hex returns the hex-encoded SHA256 of the input. Used to stamp
// LabelSSHAuthorizedKeysSHA256 so later Up()s can detect pubkey drift
// and silently re-sync.
func SHA256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
