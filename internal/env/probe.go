// Package env is the container environment logic dcx and dcx-agent share:
// the userEnvProbe (its command and output) and the system files the agent
// maintains (dcx's block of /etc/environment, the /etc/profile PATH
// patch). It is stdlib-only, because the agent binary compiles it in.
package env

import (
	"strings"
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

// ParseProbeOutput parses the probe's output: whatever the shell printed,
// the marker, then NUL-separated KEY=VALUE entries. PWD is dropped: it
// is the probe's directory, not the session's.
func ParseProbeOutput(output string) map[string]string {
	env := make(map[string]string)
	if i := strings.LastIndex(output, probeMarker+"\x00"); i >= 0 {
		output = output[i+len(probeMarker)+1:]
	}
	for _, entry := range strings.Split(output, "\x00") {
		if idx := strings.Index(entry, "="); idx > 0 {
			env[entry[:idx]] = entry[idx+1:]
		}
	}
	delete(env, "PWD")
	return env
}
