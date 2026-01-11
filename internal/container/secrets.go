package container

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/griffithind/dcx/internal/secrets"
)

const (
	// SecretsDir is the directory where secrets are mounted in the container.
	SecretsDir = "/run/secrets"
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

	// Create /run/secrets directory if it doesn't exist
	if err := createSecretsDir(ctx, containerName); err != nil {
		return fmt.Errorf("failed to create secrets directory: %w", err)
	}

	// Write each secret to the container
	for _, secret := range secretList {
		if err := writeSecretToContainer(ctx, containerName, secret); err != nil {
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

// createSecretsDir creates /run/secrets with proper permissions.
func createSecretsDir(ctx context.Context, containerName string) error {
	// Create directory as root
	cmd := exec.CommandContext(ctx, "docker", "exec", "--user", "root",
		containerName, "mkdir", "-p", SecretsDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mkdir failed: %w, output: %s", err, output)
	}

	// Set permissions (755 for directory)
	cmd = exec.CommandContext(ctx, "docker", "exec", "--user", "root",
		containerName, "chmod", "755", SecretsDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chmod failed: %w, output: %s", err, output)
	}

	return nil
}

// writeSecretToContainer writes a secret to the container's /run/secrets.
func writeSecretToContainer(ctx context.Context, containerName string, secret secrets.Secret) error {
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
	destPath := filepath.Join(SecretsDir, secret.Name)
	copyCmd := exec.CommandContext(ctx, "docker", "cp", tmpFile.Name(), containerName+":"+destPath)
	if output, err := copyCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker cp failed: %w, output: %s", err, output)
	}

	// Set permissions (400 for secret files - read-only by owner)
	chmodCmd := exec.CommandContext(ctx, "docker", "exec", "--user", "root",
		containerName, "chmod", "400", destPath)
	if output, err := chmodCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("chmod failed: %w, output: %s", err, output)
	}

	return nil
}
