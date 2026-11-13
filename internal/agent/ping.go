package agent

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
)

// pingAddr checks that a dcx agent answers at addr: it connects and reads
// the SSH banner, which must name the dcx agent. It is the liveness probe
// host-side dcx runs via `docker exec dcx-agent ping`. A bare TCP connect
// counted any listener (with host networking, an unrelated process on the
// port) as a running agent.
func pingAddr(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	banner, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read banner from %s: %w", addr, err)
	}
	if want := "SSH-2.0-" + agentproto.ServerVersion; !strings.HasPrefix(banner, want) {
		return fmt.Errorf("%s is not a dcx agent (banner %q)", addr, strings.TrimSpace(banner))
	}
	return nil
}
