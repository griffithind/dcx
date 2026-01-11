// Package secrets provides functionality for fetching and managing secrets
// from external secret managers like 1Password, Doppler, etc.
package secrets

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/griffithind/dcx/internal/devcontainer"
)

// Secret represents a fetched secret with its name and value.
type Secret struct {
	Name  string
	Value []byte
}

// Fetcher fetches secrets by executing commands on the host.
type Fetcher struct {
	// logger is used for logging secret fetch operations.
	// Note: Secret values are never logged.
	logger *slog.Logger
}

// NewFetcher creates a new secret fetcher.
func NewFetcher(logger *slog.Logger) *Fetcher {
	if logger == nil {
		logger = slog.Default()
	}
	return &Fetcher{logger: logger}
}

// FetchSecrets executes commands to fetch all configured secrets.
// Returns an error if any required secret command fails.
func (f *Fetcher) FetchSecrets(ctx context.Context, configs map[string]devcontainer.SecretConfig) ([]Secret, error) {
	if len(configs) == 0 {
		return nil, nil
	}

	result := make([]Secret, 0, len(configs))

	for name, config := range configs {
		f.logger.Debug("Fetching secret", "name", name)

		value, err := f.executeCommand(ctx, config.Command)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch secret %q: %w", name, err)
		}

		result = append(result, Secret{
			Name:  name,
			Value: value,
		})

		f.logger.Debug("Successfully fetched secret", "name", name)
	}

	return result, nil
}

// executeCommand runs a shell command and returns its stdout.
func (f *Fetcher) executeCommand(ctx context.Context, command string) ([]byte, error) {
	// Use shell to execute the command
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Include stderr in error message for debugging, but not the command output
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return nil, fmt.Errorf("command failed: %w\nstderr: %s", err, stderrStr)
		}
		return nil, fmt.Errorf("command failed: %w", err)
	}

	// Trim trailing newline from output (common for CLI tools)
	value := bytes.TrimSuffix(stdout.Bytes(), []byte("\n"))
	return value, nil
}

// WriteToTempFiles writes secrets to temporary files and returns the file paths.
// The caller is responsible for cleaning up the files.
// Returns a map of secret name to temp file path and a cleanup function.
func WriteToTempFiles(secrets []Secret, prefix string) (map[string]string, func(), error) {
	if len(secrets) == 0 {
		return nil, func() {}, nil
	}

	paths := make(map[string]string, len(secrets))
	var cleanupFuncs []func()

	cleanup := func() {
		for _, fn := range cleanupFuncs {
			fn()
		}
	}

	for _, secret := range secrets {
		path, cleanupFn, err := writeTempFile(secret, prefix)
		if err != nil {
			cleanup() // Clean up any files we've already created
			return nil, nil, fmt.Errorf("failed to write secret %q to temp file: %w", secret.Name, err)
		}
		paths[secret.Name] = path
		cleanupFuncs = append(cleanupFuncs, cleanupFn)
	}

	return paths, cleanup, nil
}
