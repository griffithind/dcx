// Package env handles user environment probing for devcontainers.
package env

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/docker/docker/client"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/state"
)

// ProbeType represents the type of shell probe to use.
type ProbeType string

const (
	// ProbeNone disables environment probing.
	ProbeNone ProbeType = "none"
	// ProbeLoginShell probes using a login shell (sh -l -c).
	ProbeLoginShell ProbeType = "loginShell"
	// ProbeLoginInteractiveShell probes using a login interactive shell (sh -l -i -c).
	ProbeLoginInteractiveShell ProbeType = "loginInteractiveShell"
	// ProbeInteractiveShell probes using an interactive shell (sh -i -c).
	ProbeInteractiveShell ProbeType = "interactiveShell"
)

// ProbeCommand returns the shell command arguments to use for probing.
func ProbeCommand(probeType ProbeType) []string {
	switch probeType {
	case ProbeLoginShell:
		return []string{"sh", "-l", "-c", "env"}
	case ProbeLoginInteractiveShell:
		return []string{"sh", "-l", "-i", "-c", "env"}
	case ProbeInteractiveShell:
		return []string{"sh", "-i", "-c", "env"}
	default:
		return nil
	}
}

// ParseProbeType parses a string into a ProbeType.
func ParseProbeType(s string) ProbeType {
	switch s {
	case "loginShell":
		return ProbeLoginShell
	case "loginInteractiveShell":
		return ProbeLoginInteractiveShell
	case "interactiveShell":
		return ProbeInteractiveShell
	case "none", "":
		return ProbeNone
	default:
		return ProbeNone
	}
}

// Prober probes container environments.
type Prober struct {
	dockerClient *client.Client
	timeout      time.Duration
}

// NewProber creates a new environment prober.
func NewProber(dockerClient *client.Client) *Prober {
	return &Prober{
		dockerClient: dockerClient,
		timeout:      10 * time.Second,
	}
}

// Probe executes the environment probe in the container and returns the environment variables.
func (p *Prober) Probe(ctx context.Context, containerID string, probeType ProbeType, user string) (map[string]string, error) {
	if probeType == ProbeNone || probeType == "" {
		return nil, nil
	}

	cmd := ProbeCommand(probeType)
	if cmd == nil {
		return nil, nil
	}

	// Execute probe with timeout
	probeCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	output, exitCode, err := container.ExecOutput(probeCtx, p.dockerClient, containerID, cmd, user)
	if err != nil {
		return nil, fmt.Errorf("failed to probe environment: %w", err)
	}

	if exitCode != 0 {
		return nil, fmt.Errorf("environment probe exited with code %d", exitCode)
	}

	// Parse environment output
	env := parseEnvOutput(output)

	// Remove PWD as it's not meaningful for exec context
	delete(env, "PWD")

	return env, nil
}

// parseEnvOutput parses the output of 'env' command into a map.
func parseEnvOutput(output string) map[string]string {
	env := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "="); idx > 0 {
			key := line[:idx]
			value := line[idx+1:]
			env[key] = value
		}
	}
	return env
}

// ProbeWithCache probes the user environment with caching support.
// If a valid cached result exists for the given imageHash, it returns the cached value.
// Otherwise, it probes fresh and caches the result in container labels.
func (p *Prober) ProbeWithCache(ctx context.Context, containerID string, probeType ProbeType, user string, imageHash string) (map[string]string, error) {
	if probeType == ProbeNone || probeType == "" {
		return nil, nil
	}

	// Check cache first
	cachedEnv, cachedHash, err := p.readCache(ctx, containerID)
	if err == nil && cachedHash == imageHash && len(cachedEnv) > 0 {
		return cachedEnv, nil
	}

	// Probe fresh
	env, err := p.Probe(ctx, containerID, probeType, user)
	if err != nil {
		return nil, err
	}

	// Cache the result
	if env != nil && imageHash != "" {
		if cacheErr := p.writeCache(ctx, containerID, env, imageHash); cacheErr != nil {
			// Log but don't fail - caching is best-effort
			_ = cacheErr
		}
	}

	return env, nil
}

// readCache reads the cached probed environment from container labels.
func (p *Prober) readCache(ctx context.Context, containerID string) (map[string]string, string, error) {
	inspect, err := p.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, "", err
	}

	labels := inspect.Config.Labels
	if labels == nil {
		return nil, "", fmt.Errorf("no labels found")
	}

	hash := labels[state.LabelCacheProbedEnvHash]
	envData := labels[state.LabelCacheProbedEnv]

	if envData == "" {
		return nil, hash, fmt.Errorf("no cached env found")
	}

	var env map[string]string
	if err := json.Unmarshal([]byte(envData), &env); err != nil {
		return nil, hash, err
	}

	return env, hash, nil
}

// writeCache writes the probed environment to container labels.
func (p *Prober) writeCache(ctx context.Context, containerID string, env map[string]string, imageHash string) error {
	envData, err := json.Marshal(env)
	if err != nil {
		return err
	}

	// Update container labels using docker update
	// Note: Docker doesn't support updating labels directly, so we use a workaround
	// by storing in a well-known location in the container filesystem
	cmd := []string{"sh", "-c", fmt.Sprintf(`
mkdir -p /var/lib/dcx && \
echo '%s' > /var/lib/dcx/probed-env.json && \
echo '%s' > /var/lib/dcx/probed-env-hash
`, string(envData), imageHash)}

	_, exitCode, err := container.ExecOutput(ctx, p.dockerClient, containerID, cmd, "root")
	if err != nil {
		return err
	}
	if exitCode != 0 {
		return fmt.Errorf("failed to cache probed env: exit code %d", exitCode)
	}

	return nil
}

// ReadCachedEnv reads the cached probed environment from the container filesystem.
// This is used when container labels aren't available (e.g., after container restart).
func (p *Prober) ReadCachedEnv(ctx context.Context, containerID string, imageHash string) (map[string]string, error) {
	// First check container labels
	env, cachedHash, err := p.readCache(ctx, containerID)
	if err == nil && cachedHash == imageHash {
		return env, nil
	}

	// Fall back to filesystem cache
	cmd := []string{"sh", "-c", "cat /var/lib/dcx/probed-env-hash 2>/dev/null && cat /var/lib/dcx/probed-env.json 2>/dev/null"}
	output, exitCode, err := container.ExecOutput(ctx, p.dockerClient, containerID, cmd, "root")
	if err != nil || exitCode != 0 {
		return nil, fmt.Errorf("no cached env found")
	}

	lines := strings.SplitN(output, "\n", 2)
	if len(lines) < 2 {
		return nil, fmt.Errorf("invalid cache format")
	}

	cachedHash = strings.TrimSpace(lines[0])
	if cachedHash != imageHash {
		return nil, fmt.Errorf("cache hash mismatch: %s != %s", cachedHash, imageHash)
	}

	if err := json.Unmarshal([]byte(lines[1]), &env); err != nil {
		return nil, err
	}

	return env, nil
}

// UpdateContainerLabels updates container labels with the probed environment.
// This should be called when creating/recreating a container to include cached env in labels.
func UpdateContainerLabels(labels map[string]string, env map[string]string, imageHash string) {
	if len(env) == 0 {
		return
	}

	envData, err := json.Marshal(env)
	if err != nil {
		return
	}

	labels[state.LabelCacheProbedEnv] = string(envData)
	labels[state.LabelCacheProbedEnvHash] = imageHash
}
