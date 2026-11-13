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
	bin, err := dcxembed.GetBinary("amd64")
	if err != nil || len(bin) == 0 {
		t.Skip("embedded agent binaries not built (run 'make build')")
	}
	return bin
}

func TestDeployInstallsIntoRootOwnedDir(t *testing.T) {
	bin := agentBinary(t)
	fake := &containertest.Fake{Outputs: map[string]string{"uname -m": "x86_64\n"}}

	installed, err := Deploy(context.Background(), fake, "c1")
	require.NoError(t, err)
	assert.True(t, installed)

	root := fake.CallsTo("ExecAsRoot")
	require.Len(t, root, 2)
	assert.Equal(t, []string{"c1", "install", "-d", "-o", "root", "-g", "root", "-m", "0755", agentproto.BinaryDir}, root[0].Args)
	// Paths and digest are positional arguments of a fixed script.
	assert.Equal(t, []string{agentproto.BinaryPath + ".new", agentproto.BinaryPath, sha256Hex(bin)}, root[1].Args[len(root[1].Args)-3:])
	assert.Equal(t, bin, fake.Files["c1:"+agentproto.BinaryPath+".new"], "the staged copy is the embedded binary")
}

func TestDeploySkipsMatchingBinary(t *testing.T) {
	bin := agentBinary(t)
	digestQuery := strings.Join([]string{"sh", "-c", `test -x "$1" && cat "$1.sha256"`, "dcx-deploy", agentproto.BinaryPath}, " ")
	fake := &containertest.Fake{Outputs: map[string]string{
		"uname -m":  "x86_64",
		digestQuery: sha256Hex(bin) + "\n",
	}}

	installed, err := Deploy(context.Background(), fake, "c1")
	require.NoError(t, err)
	assert.False(t, installed)
	assert.Empty(t, fake.CallsTo("CopyToContainer"))
}

func TestStopOutdatedWaitsForListenerToGo(t *testing.T) {
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
	require.NoError(t, stopOutdated(context.Background(), fake, "c1", agentproto.BinaryPath, agentproto.LoopbackAddr, 5*time.Second))
	assert.Equal(t, 3, pings)

	kill := fake.CallsTo("ExecAsRoot")
	require.Len(t, kill, 1)
	assert.Equal(t, []string{agentproto.BinaryPath, agentproto.LegacyBinaryPath}, kill[0].Args[len(kill[0].Args)-2:])
}

func TestStopOutdatedTimesOut(t *testing.T) {
	fake := &containertest.Fake{} // ping always succeeds
	err := stopOutdated(context.Background(), fake, "c1", agentproto.BinaryPath, agentproto.LoopbackAddr, 300*time.Millisecond)
	assert.ErrorContains(t, err, "still listening")
}
