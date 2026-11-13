// Package env handles user environment probing for devcontainers.
package env

import (
	"bufio"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/container"
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
	engine  container.Engine
	timeout time.Duration
}

// NewProber creates an environment prober that runs through engine.
func NewProber(engine container.Engine) *Prober {
	return &Prober{engine: engine, timeout: 10 * time.Second}
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

	output, err := p.engine.ExecOutputAs(probeCtx, containerID, user, cmd)
	if err != nil {
		return nil, fmt.Errorf("failed to probe environment: %w", err)
	}

	env := parseEnvOutput(string(output))

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
