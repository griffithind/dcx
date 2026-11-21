// Package secrets provides functionality for fetching and managing secrets
// from external secret managers like 1Password, Doppler, etc.
package secrets

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/griffithind/dcx/internal/devcontainer"
)

// Secret represents a fetched secret with its name and value.
type Secret struct {
	Name  string
	Value []byte
}

// reservedSecretNames cannot be used for user secrets: "dcx" is the
// directory holding dcx-managed secrets (SSH host key, authorized_keys).
var reservedSecretNames = map[string]bool{"dcx": true}

// ValidateName checks that a secret name is safe to use as a file name
// directly under /run/secrets. Names come from devcontainer.json, which is
// repository-controlled, so anything that could escape the directory or be
// interpreted specially is rejected.
func ValidateName(name string) error {
	if name == "" || name == "." || name == ".." {
		return fmt.Errorf("invalid secret name %q", name)
	}
	if reservedSecretNames[name] {
		return fmt.Errorf("invalid secret name %q: reserved for dcx", name)
	}
	for _, c := range name {
		ok := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.'
		if !ok {
			return fmt.Errorf("invalid secret name %q: only letters, digits, '.', '_' and '-' are allowed", name)
		}
	}
	return nil
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

// FetchSecrets runs the commands of all configured secrets and returns
// them sorted by name. The commands run at the same time: each is usually
// a call to a secret manager's CLI (op, doppler, vault) taking a large
// part of a second. When one fails, the others are cancelled and the error
// names the first failing secret in name order.
func (f *Fetcher) FetchSecrets(ctx context.Context, configs map[string]devcontainer.SecretConfig) ([]Secret, error) {
	if len(configs) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(configs))
	for name := range configs {
		if err := ValidateName(name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	sort.Strings(names)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make([]Secret, len(names))
	errs := make([]error, len(names))
	// cancelled[i]: secret i failed after another failure cancelled it.
	cancelled := make([]bool, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f.logger.Debug("Fetching secret", "name", name)
			value, err := f.executeCommand(ctx, string(configs[name]))
			if err != nil {
				errs[i], cancelled[i] = err, ctx.Err() != nil
				cancel()
				return
			}
			result[i] = Secret{Name: name, Value: value}
			f.logger.Debug("Successfully fetched secret", "name", name)
		}()
	}
	wg.Wait()
	for _, includeCancelled := range []bool{false, true} {
		for i, err := range errs {
			if err != nil && (includeCancelled || !cancelled[i]) {
				return nil, fmt.Errorf("failed to fetch secret %q: %w", names[i], err)
			}
		}
	}
	return result, nil
}

// executeCommand runs a shell command and returns its stdout.
func (f *Fetcher) executeCommand(ctx context.Context, command string) ([]byte, error) {
	// Use shell to execute the command
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	// Cancelling kills the shell; a command it started (op, sleep) may
	// still hold the output open, so stop waiting for it shortly after.
	cmd.WaitDelay = 500 * time.Millisecond

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
