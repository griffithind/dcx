// Package proxy provides utilities for bidirectional network proxying.
package proxy

import (
	"io"
	"net"
	"sync"
)

// BidirectionalCopy copies data between two connections until one side closes.
// It waits for both copy directions to complete before returning.
// Half-close is signaled when supported by the connection type (e.g., TCP, Unix sockets).
//
// This function does not close the connections - callers should defer Close() on both.
func BidirectionalCopy(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copyAndSignal := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src) //nolint:errcheck // errors expected on connection close
		// Signal half-close if the connection supports it
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			c.CloseWrite() //nolint:errcheck // best-effort half-close
		}
	}

	go copyAndSignal(a, b)
	go copyAndSignal(b, a)
	wg.Wait()
}
