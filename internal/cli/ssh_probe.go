package cli

import (
	"context"
	"fmt"
	"net"
	"time"
)

// tcpReachable attempts a short TCP handshake to host:port, returning nil
// if it succeeds within 2 seconds. Used by `dcx ssh info --doctor`.
func tcpReachable(ctx context.Context, host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	return conn.Close()
}
