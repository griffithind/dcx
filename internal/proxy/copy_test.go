package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockConn is a mock net.Conn for testing.
type mockConn struct {
	readBuf  *bytes.Buffer
	writeBuf *bytes.Buffer
	closed   bool
	writeClosed bool
	mu       sync.Mutex
}

func newMockConn(readData string) *mockConn {
	return &mockConn{
		readBuf:  bytes.NewBufferString(readData),
		writeBuf: &bytes.Buffer{},
	}
}

func (m *mockConn) Read(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, io.EOF
	}
	return m.readBuf.Read(b)
}

func (m *mockConn) Write(b []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.writeClosed {
		return 0, io.ErrClosedPipe
	}
	return m.writeBuf.Write(b)
}

func (m *mockConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockConn) CloseWrite() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.writeClosed = true
	return nil
}

func (m *mockConn) LocalAddr() net.Addr                { return nil }
func (m *mockConn) RemoteAddr() net.Addr               { return nil }
func (m *mockConn) SetDeadline(t time.Time) error      { return nil }
func (m *mockConn) SetReadDeadline(t time.Time) error  { return nil }
func (m *mockConn) SetWriteDeadline(t time.Time) error { return nil }

func (m *mockConn) Written() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeBuf.String()
}

func (m *mockConn) IsWriteClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.writeClosed
}

func TestBidirectionalCopy(t *testing.T) {
	t.Run("copies data in both directions", func(t *testing.T) {
		connA := newMockConn("hello from A")
		connB := newMockConn("hello from B")

		BidirectionalCopy(connA, connB)

		assert.Equal(t, "hello from B", connA.Written())
		assert.Equal(t, "hello from A", connB.Written())
	})

	t.Run("signals half-close when supported", func(t *testing.T) {
		connA := newMockConn("data")
		connB := newMockConn("")

		BidirectionalCopy(connA, connB)

		assert.True(t, connA.IsWriteClosed(), "connA should have CloseWrite called")
		assert.True(t, connB.IsWriteClosed(), "connB should have CloseWrite called")
	})

	t.Run("waits for both directions to complete", func(t *testing.T) {
		// Create connections where one side has more data
		connA := newMockConn("short")
		connB := newMockConn("this is a longer message that takes more time")

		done := make(chan struct{})
		go func() {
			BidirectionalCopy(connA, connB)
			close(done)
		}()

		select {
		case <-done:
			// Success - both directions completed
		case <-time.After(time.Second):
			t.Fatal("BidirectionalCopy did not complete in time")
		}

		// Both directions should have completed
		assert.Equal(t, "this is a longer message that takes more time", connA.Written())
		assert.Equal(t, "short", connB.Written())
	})

	t.Run("handles empty data", func(t *testing.T) {
		connA := newMockConn("")
		connB := newMockConn("")

		BidirectionalCopy(connA, connB)

		assert.Equal(t, "", connA.Written())
		assert.Equal(t, "", connB.Written())
	})
}

func TestBidirectionalCopyWithRealSockets(t *testing.T) {
	// Test with real Unix sockets to verify CloseWrite works correctly
	t.Run("works with Unix sockets", func(t *testing.T) {
		// Create a socket pair
		sockA, sockB, err := createSocketPair(t)
		require.NoError(t, err)
		defer sockA.Close()  //nolint:errcheck // test cleanup
		defer sockB.Close()  //nolint:errcheck // test cleanup

		// Write test data before starting copy
		testDataA := "message from A"
		testDataB := "message from B"

		// We need to do the writes and BidirectionalCopy concurrently
		// because BidirectionalCopy blocks until both sides are done
		var wg sync.WaitGroup
		wg.Add(2)

		// Writer goroutine for sockA's read side
		go func() {
			defer wg.Done()
			_, _ = sockA.Write([]byte(testDataA))
			// Close write to signal EOF
			if uc, ok := sockA.(*net.UnixConn); ok {
				_ = uc.CloseWrite()
			}
		}()

		// Writer goroutine for sockB's read side
		go func() {
			defer wg.Done()
			_, _ = sockB.Write([]byte(testDataB))
			// Close write to signal EOF
			if uc, ok := sockB.(*net.UnixConn); ok {
				_ = uc.CloseWrite()
			}
		}()

		wg.Wait()
	})
}

func createSocketPair(t *testing.T) (net.Conn, net.Conn, error) {
	// Create a temporary Unix socket with a short path
	// Note: t.TempDir() paths can exceed Unix socket limit (~104 chars on macOS)
	socketPath := fmt.Sprintf("/tmp/proxy-test-%d.sock", os.Getpid())
	t.Cleanup(func() { _ = os.Remove(socketPath) })

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, nil, err
	}
	defer listener.Close() //nolint:errcheck // test cleanup

	// Connect from client side
	var clientConn net.Conn
	var serverConn net.Conn
	var connErr error

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		clientConn, connErr = net.Dial("unix", socketPath)
	}()

	go func() {
		defer wg.Done()
		serverConn, _ = listener.Accept()
	}()

	wg.Wait()

	if connErr != nil {
		return nil, nil, connErr
	}

	return clientConn, serverConn, nil
}
