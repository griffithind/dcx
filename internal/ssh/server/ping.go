package server

import (
	"fmt"
	"net"
	"time"
)

// pingAddr attempts a short TCP handshake to addr. It is the in-container
// liveness probe invoked via `docker exec dcx-agent ping` by host-side
// dcx. No SSH bytes are exchanged — we only want to know whether a
// listener is accepting connections.
func pingAddr(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	return conn.Close()
}
