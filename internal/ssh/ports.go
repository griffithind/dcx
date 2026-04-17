// Package ssh — ports.go derives a stable host port for each workspace's
// SSH listener, so `dcx down` + `dcx up` returns the same 127.0.0.1:<port>
// instead of rotating ephemerals.
//
// Why this matters: IDE clients key `known_hosts` entries, persistent
// sessions, and user bookmarks by (hostname, port). A rotating port looks
// like "connection refused" or a host-key-changed warning even though dcx
// is doing its job.
//
// The port is a pure function of the workspace ID — no state file, no
// labels, no persistence. Any dcx invocation for the same workspace
// computes the same port. The service layer probes availability before
// committing; if the derived port is bound by something else we fall back
// to a Docker-picked ephemeral and the user sees a yellow warning.
package ssh

import (
	"fmt"
	"hash/fnv"
	"net"
)

// The port range is chosen to minimize collisions with common local apps:
//
//   - Below 40000: mostly IANA-registered (databases, web servers, etc.)
//   - Above 49151: ephemeral range (OS-picked client-side ports)
//   - 40000-48000: largely unassigned in the IANA registry
//
// 8000 slots gives a birthday-bound 50% collision at ~sqrt(16000) ≈ 126
// workspaces on the same machine. Realistic upper bound is well below that.
const (
	sshPortBase  = 40000
	sshPortRange = 8000 // [40000, 48000)
)

// DeterministicPort returns the host port dcx prefers for this workspace.
// Two invocations with the same workspaceID always return the same port.
// Callers should check [IsHostPortAvailable] before requesting it from
// Docker — if the port is taken, fall back to an ephemeral.
func DeterministicPort(workspaceID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(workspaceID))
	return sshPortBase + int(h.Sum32()%sshPortRange)
}

// IsHostPortAvailable probes whether the given TCP port is currently free
// on 127.0.0.1. There is an inherent TOCTOU gap between this probe and
// `docker run`, but the gap is microseconds and the fallback (ephemeral)
// handles the race harmlessly.
func IsHostPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
