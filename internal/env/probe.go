// Package env handles user environment probing for devcontainers.
package env

import (
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
	// ProbeLoginShell probes using the user's login shell as a login shell.
	ProbeLoginShell ProbeType = "loginShell"
	// ProbeLoginInteractiveShell probes using the user's shell as a login,
	// interactive shell (the spec default).
	ProbeLoginInteractiveShell ProbeType = "loginInteractiveShell"
	// ProbeInteractiveShell probes using the user's shell as an
	// interactive shell.
	ProbeInteractiveShell ProbeType = "interactiveShell"
)

// probeScript runs the user's shell as sessions do (the container's SHELL,
// else the user's login shell from the passwd database, else /bin/sh; a
// nologin or false shell counts as none) with
// the probe's flags ("$@") and prints its
// environment: a marker, then /proc/self/environ. The environment is
// NUL-separated, so multi-line values (exported bash functions) survive,
// and the marker separates it from anything the shell's startup files
// print. Nothing is interpolated into the script.
const probeScript = `shell=$SHELL
if [ -z "$shell" ] || [ ! -x "$shell" ]; then
	user=$(id -un)
	shell=$(getent passwd "$user" 2>/dev/null | cut -d: -f7)
	[ -n "$shell" ] || shell=$(awk -F: -v u="$user" '$1 == u { print $7 }' /etc/passwd 2>/dev/null)
fi
case "$shell" in */nologin|*/false) shell= ;; esac
[ -n "$shell" ] && [ -x "$shell" ] || shell=/bin/sh
exec "$shell" "$@" -c 'printf "\n` + probeMarker + `\0"; cat /proc/self/environ'`

// probeMarker precedes the environment in the probe's output.
const probeMarker = "__DCX_ENV__"

// ProbeCommand returns the command that probes the environment, or nil
// for ProbeNone.
func ProbeCommand(probeType ProbeType) []string {
	var flags []string
	switch probeType {
	case ProbeLoginShell:
		flags = []string{"-l"}
	case ProbeLoginInteractiveShell:
		flags = []string{"-l", "-i"}
	case ProbeInteractiveShell:
		flags = []string{"-i"}
	default:
		return nil
	}
	return append([]string{"sh", "-c", probeScript, "dcx-probe"}, flags...)
}

// ParseProbeType parses userEnvProbe. Unset (and unknown values) mean
// loginInteractiveShell, the spec's default.
func ParseProbeType(s string) ProbeType {
	switch s {
	case "none":
		return ProbeNone
	case "loginShell":
		return ProbeLoginShell
	case "interactiveShell":
		return ProbeInteractiveShell
	default:
		return ProbeLoginInteractiveShell
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

// parseEnvOutput parses the probe's output: whatever the shell printed,
// the marker, then NUL-separated KEY=VALUE entries.
func parseEnvOutput(output string) map[string]string {
	env := make(map[string]string)
	if i := strings.LastIndex(output, probeMarker+"\x00"); i >= 0 {
		output = output[i+len(probeMarker)+1:]
	}
	for _, entry := range strings.Split(output, "\x00") {
		if idx := strings.Index(entry, "="); idx > 0 {
			env[entry[:idx]] = entry[idx+1:]
		}
	}
	return env
}
