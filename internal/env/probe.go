// Package env handles user environment probing for devcontainers.
package env

import (
	"bufio"
	"context"
	"fmt"
	"strings"

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

// ProbeCommand returns the shell command to use for probing.
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

// Prober probes container environments.
type Prober struct {
	dockerClient *container.DockerClient
}

// NewProber creates a new environment prober.
func NewProber(dockerClient *container.DockerClient) *Prober {
	return &Prober{dockerClient: dockerClient}
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

	// Capture output
	var output strings.Builder
	execConfig := container.ExecConfig{
		ContainerID: containerID,
		Cmd:         cmd,
		User:        user,
		Stdout:      &output,
		Stderr:      &output,
	}

	exitCode, err := container.Exec(ctx, p.dockerClient.APIClient(), execConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to probe environment: %w", err)
	}

	if exitCode != 0 {
		return nil, fmt.Errorf("environment probe exited with code %d", exitCode)
	}

	// Parse environment output
	env := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(output.String()))
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "="); idx > 0 {
			key := line[:idx]
			value := line[idx+1:]
			env[key] = value
		}
	}

	return env, nil
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
