package agentctl

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	dcxembed "github.com/griffithind/dcx"
	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agentBinary(t *testing.T) []byte {
	t.Helper()
	bin, err := dcxembed.Binary("amd64")
	if err != nil || len(bin) == 0 {
		t.Skip("embedded agent binaries not built (run 'make build')")
	}
	return bin
}

// probeKey is the probe exec as the fake engine records it.
var probeKey = strings.Join([]string{"sh", "-c", probeScript, "dcx-deploy", agentproto.BinaryPath}, " ")

func TestDeployInstallsIntoRootOwnedDir(t *testing.T) {
	bin := agentBinary(t)
	fake := &containertest.Fake{Outputs: map[string]string{probeKey: "x86_64\n"}}

	installed, err := Deploy(context.Background(), fake, "c1")
	require.NoError(t, err)
	assert.True(t, installed)

	// Three execs: the probe (which also creates the root-owned binary
	// directory), the copy, and the install (which also signals outdated
	// agents).
	root := fake.CallsTo("ExecAsRoot")
	require.Len(t, root, 2)
	assert.Equal(t, []string{"c1", "sh", "-c", probeScript, "dcx-deploy", agentproto.BinaryPath}, root[0].Args)
	assert.Contains(t, probeScript, `install -d -o root -g root -m 0755`)
	// Paths and digest are positional arguments of a fixed script.
	assert.Equal(t, []string{agentproto.BinaryPath + ".new", agentproto.BinaryPath, sha256Hex(bin), agentproto.LegacyBinaryPath},
		root[1].Args[len(root[1].Args)-4:])
	assert.Len(t, fake.CallsTo("CopyToContainer"), 1)
	assert.Equal(t, bin, fake.Files["c1:"+agentproto.BinaryPath+".new"], "the staged copy is the embedded binary")
}

func TestDeploySkipsMatchingBinary(t *testing.T) {
	bin := agentBinary(t)
	fake := &containertest.Fake{Outputs: map[string]string{probeKey: "x86_64\n" + sha256Hex(bin) + "\n"}}

	installed, err := Deploy(context.Background(), fake, "c1")
	require.NoError(t, err)
	assert.False(t, installed)
	assert.Empty(t, fake.CallsTo("CopyToContainer"))
	assert.Len(t, fake.CallsTo("ExecAsRoot"), 1, "a current agent costs one exec")
}

func TestWaitGoneWaitsForListenerToGo(t *testing.T) {
	pings := 0
	fake := &containertest.Fake{ExecFunc: func(_ string, argv []string) error {
		if len(argv) > 1 && argv[1] == "ping" {
			pings++
			if pings < 3 {
				return nil // old agent still answering
			}
			return errors.New("connection refused")
		}
		return nil
	}}
	require.NoError(t, waitGone(context.Background(), fake, "c1", agentproto.BinaryPath, agentproto.LoopbackAddr, 5*time.Second))
	assert.Equal(t, 3, pings)
}

func TestWaitGoneTimesOut(t *testing.T) {
	fake := &containertest.Fake{} // ping always succeeds
	err := waitGone(context.Background(), fake, "c1", agentproto.BinaryPath, agentproto.LoopbackAddr, 300*time.Millisecond)
	assert.ErrorContains(t, err, "still listening")
}
