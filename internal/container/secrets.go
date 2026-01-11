package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/secrets"
)

// MountSecretsToContainer copies secrets to /run/secrets in the specified container.
// Secrets are written as files with mode 0400 (read-only by owner).
// This is a standalone function that can be called without a runtime instance.
func MountSecretsToContainer(ctx context.Context, containerName string, secretList []secrets.Secret) error {
	if len(secretList) == 0 {
		return nil
	}

	if containerName == "" {
		return fmt.Errorf("container name not set")
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
		if err := writeSecretToContainer(ctx, docker, containerName, secret); err != nil {
			return fmt.Errorf("failed to write secret %q: %w", secret.Name, err)
		}
	}

	return nil
}

// MountSecrets copies secrets to /run/secrets in the container.
// Secrets are written as files with mode 0400 (read-only by owner).
func (r *UnifiedRuntime) MountSecrets(ctx context.Context, secretList []secrets.Secret) error {
	return MountSecretsToContainer(ctx, r.containerName, secretList)
}

// writeSecretToContainer writes a secret to the container's /run/secrets.
func writeSecretToContainer(ctx context.Context, docker *Docker, containerName string, secret secrets.Secret) error {
	// Write secret to temp file
	tmpFile, err := os.CreateTemp("", "dcx-secret-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := tmpFile.Write(secret.Value); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Copy to container
	destPath := filepath.Join(common.SecretsDir, secret.Name)
	if err := docker.CopyToContainer(ctx, tmpFile.Name(), containerName, destPath); err != nil {
		return err
	}

	// Set permissions (400 for secret files - read-only by owner)
	if err := docker.ChmodInContainer(ctx, containerName, destPath, "400", "root"); err != nil {
		return err
	}

	return nil
}
