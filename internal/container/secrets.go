package container

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/secrets"
)

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

// MountSecrets copies secrets to /run/secrets in the container.
// Secrets are written as files with mode 0400 (read-only by owner).
func (r *UnifiedRuntime) MountSecrets(ctx context.Context, secretList []secrets.Secret, owner string) error {
	return MountSecretsToContainer(ctx, r.containerName, secretList, owner)
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
