// Package ssh — ports.go derives a stable host port for each workspace's
// SSH listener, so `dcx down` + `dcx up` returns the same 127.0.0.1:<port>
// instead of rotating ephemerals.
//
// Why this matters: IDE clients key `known_hosts` entries, persistent
// sessions, and user bookmarks by (hostname, port). A rotating port looks
// like "connection refused" or a host-key-changed warning even though dcx
// is doing its job.
//
// The preferred port is a pure function of the workspace ID. When it is
// taken (by another process, or recorded by another workspace's container,
// stopped or not) [PickPort] probes the following ports in order, so the
// result is still stable as long as the neighbourhood doesn't change. The
// chosen port is recorded on the container (state.LabelSSHPort).
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

// maxPortProbes bounds how many ports PickPort tries after the preferred one.
const maxPortProbes = 64

// DeterministicPort returns the host port dcx prefers for this workspace.
// Two invocations with the same workspaceID always return the same port.
func DeterministicPort(workspaceID string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(workspaceID))
	return sshPortBase + int(h.Sum32()%sshPortRange)
}

// PickPort returns the first port, starting at the workspace's
// DeterministicPort and wrapping within the range, for which free reports
// true.
func PickPort(workspaceID string, free func(port int) bool) (int, error) {
	start := DeterministicPort(workspaceID)
	for i := 0; i < maxPortProbes; i++ {
		port := sshPortBase + (start-sshPortBase+i)%sshPortRange
		if free(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("no free SSH port in %d..%d (tried %d ports)", start, start+maxPortProbes-1, maxPortProbes)
}

// IsHostPortAvailable probes whether the given TCP port is currently free
// on 127.0.0.1. There is an inherent TOCTOU gap between this probe and
// `docker run`; losing that race fails the create with "port is already
// allocated", and the next `dcx up` picks another port.
func IsHostPortAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}
