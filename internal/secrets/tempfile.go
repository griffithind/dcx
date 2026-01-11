package secrets

import (
	"fmt"
	"os"
)

// writeTempFile writes a secret to a temporary file with restrictive permissions.
// Returns the file path and a cleanup function that removes the file.
func writeTempFile(secret Secret, prefix string) (string, func(), error) {
	// Create temp file with prefix for identification
	pattern := fmt.Sprintf("%s-%s-*", prefix, secret.Name)
	tmpFile, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	path := tmpFile.Name()

	// Set restrictive permissions (read-only for owner)
	if err := os.Chmod(path, 0600); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("failed to set permissions: %w", err)
	}

	// Write the secret value
	if _, err := tmpFile.Write(secret.Value); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("failed to write secret: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, fmt.Errorf("failed to close temp file: %w", err)
	}

	cleanup := func() {
		_ = os.Remove(path)
	}

	return path, cleanup, nil
}
