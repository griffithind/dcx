package agentctl

import (
	"context"
	"errors"
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

func TestDeployInstallsIntoRootOwnedDir(t *testing.T) {
	bin := agentBinary(t)
	fake := &containertest.Fake{}

	installed, err := Deploy(context.Background(), fake, "c1", true, true)
	require.NoError(t, err)
	assert.True(t, installed)

	// One copy writes the root-owned directories, the binary and, last, its
	// digest; a new container has no agent to check or stop, so nothing
	// else runs.
	writes := fake.CallsTo("WriteFiles")
	require.Len(t, writes, 1)
	assert.Equal(t, []string{"c1", agentproto.StateDir, agentproto.BinaryDir, agentproto.BinaryPath, agentproto.BinaryPath + ".sha256"}, writes[0].Args)
	assert.Equal(t, bin, fake.Files["c1:"+agentproto.BinaryPath], "the copy is the embedded binary (the daemon's architecture)")
	digest, err := dcxembed.Digest("amd64")
	require.NoError(t, err)
	assert.Equal(t, digest+"\n", string(fake.Files["c1:"+agentproto.BinaryPath+".sha256"]))
	assert.Empty(t, fake.CallsTo("ExecAsRoot"))
	assert.Empty(t, fake.CallsTo("ReadFile"))
}

func TestDeployStopsOutdatedAgentInRunningContainer(t *testing.T) {
	agentBinary(t)
	fake := &containertest.Fake{Files: map[string][]byte{"c1:" + agentproto.BinaryPath + ".sha256": []byte("old\n")}}

	installed, err := Deploy(context.Background(), fake, "c1", false, false)
	require.NoError(t, err)
	assert.True(t, installed)
	root := fake.CallsTo("ExecAsRoot")
	require.Len(t, root, 1)
	// The paths are positional arguments of a fixed script.
	assert.Equal(t, []string{"c1", "sh", "-c", signalOutdatedScript, "dcx-deploy", agentproto.BinaryPath, agentproto.LegacyBinaryPath}, root[0].Args)
}

func TestDeploySkipsMatchingBinary(t *testing.T) {
	agentBinary(t)
	for _, arch := range []string{"amd64", "arm64"} {
		digest, err := dcxembed.Digest(arch)
		require.NoError(t, err)
		fake := &containertest.Fake{Files: map[string][]byte{"c1:" + agentproto.BinaryPath + ".sha256": []byte(digest + "\n")}}

		installed, err := Deploy(context.Background(), fake, "c1", false, false)
		require.NoError(t, err)
		assert.False(t, installed)
		assert.Empty(t, fake.CallsTo("WriteFiles"))
		assert.Empty(t, fake.CallsTo("ExecAsRoot"))
		assert.Len(t, fake.CallsTo("ReadFile"), 1, "a current agent costs one read")
	}
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
