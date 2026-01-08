package util

import (
	"io"
	"sync"
)

// BidirectionalCopy copies data between two connections in both directions.
// It blocks until both copies complete or an error occurs.
// Both connections are NOT closed by this function - caller is responsible for closing.
func BidirectionalCopy(conn1, conn2 io.ReadWriter) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(conn1, conn2)
	}()

	go func() {
		defer wg.Done()
		io.Copy(conn2, conn1)
	}()

	wg.Wait()
}

// BidirectionalCopyFirst copies data between two connections in both directions.
// It returns as soon as one direction completes (useful for TCP proxies).
// Both connections are NOT closed by this function - caller is responsible for closing.
func BidirectionalCopyFirst(conn1, conn2 io.ReadWriter) {
	done := make(chan struct{}, 2)

	go func() {
		io.Copy(conn1, conn2)
		done <- struct{}{}
	}()

	go func() {
		io.Copy(conn2, conn1)
		done <- struct{}{}
	}()

	<-done
}
