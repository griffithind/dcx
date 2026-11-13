package agentctl

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func agentOpts(cidrs ...string) Options {
	return Options{ContainerName: "c1", WorkspaceID: "wk_test", User: "dev", WorkDir: "/workspaces/app", AllowedCIDRs: cidrs}
}

func isPing(argv []string) bool {
	return len(argv) >= 2 && argv[0] == agentproto.BinaryPath && argv[1] == "ping"
}

func TestLaunchSkipsWhenAlreadyListening(t *testing.T) {
	fake := &containertest.Fake{}
	t.Setenv("HOME", t.TempDir())

	err := launch(context.Background(), fake, agentOpts())
	require.NoError(t, err)
	assert.Empty(t, fake.CallsTo("ExecDetachedAsRoot"))
}


func TestLaunchStartsAndWaitsForReadiness(t *testing.T) {
	var pings atomic.Int32
	fake := &containertest.Fake{ExecFunc: func(_ string, argv []string) error {
		// Not listening on the idempotency probe and the first two readiness
		// polls; listening afterwards.
		if isPing(argv) && pings.Add(1) <= 3 {
			return errors.New("connection refused")
		}
		return nil
	}}
	t.Setenv("HOME", t.TempDir())

	err := launch(context.Background(), fake, agentOpts("10.0.0.0/24"))
	require.NoError(t, err)

	launches := fake.CallsTo("ExecDetachedAsRoot")
	require.Len(t, launches, 1)
	argv := strings.Join(launches[0].Args, " ")
	assert.Contains(t, argv, agentproto.BinaryPath+" listen")
	assert.Contains(t, argv, "--addr "+agentproto.ListenAddr("0.0.0.0"))
	assert.Contains(t, argv, "--host-key "+agentproto.HostKeyPath)
	assert.Contains(t, argv, "--authorized-keys "+agentproto.AuthorizedKeysPath)
	assert.Contains(t, argv, "--user dev")
	assert.Contains(t, argv, "--workdir /workspaces/app")
	assert.Contains(t, argv, "--allow-cidrs 10.0.0.0/24")
	assert.GreaterOrEqual(t, pings.Load(), int32(4), "launch must wait until the agent answers")
}

func TestLaunchFailsWhenAgentNeverListens(t *testing.T) {
	old := readyTimeout
	readyTimeout = 300 * time.Millisecond
	t.Cleanup(func() { readyTimeout = old })

	fake := &containertest.Fake{ExecFunc: func(_ string, argv []string) error {
		if isPing(argv) {
			return errors.New("connection refused")
		}
		return nil
	}}
	t.Setenv("HOME", t.TempDir())

	err := launch(context.Background(), fake, agentOpts())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not start listening")
}

func TestWriteSecretsWritesKeysWithOwnerAndModes(t *testing.T) {
	fake := &containertest.Fake{}
	t.Setenv("HOME", t.TempDir())

	err := writeSecrets(context.Background(), fake, agentOpts())
	require.NoError(t, err)

	hostKey, ok := fake.Files["c1:"+agentproto.HostKeyPath]
	require.True(t, ok, "host key must be written")
	assert.Contains(t, string(hostKey), "PRIVATE KEY")
	authKeys, ok := fake.Files["c1:"+agentproto.AuthorizedKeysPath]
	require.True(t, ok, "authorized_keys must be written")
	assert.Contains(t, string(authKeys), "ssh-ed25519", "the dcx fallback key is always authorized")

	modes := map[string]string{}
	for _, c := range fake.CallsTo("ChmodInContainer") {
		modes[c.Args[1]] = c.Args[2]
	}
	assert.Equal(t, "0400", modes[agentproto.HostKeyPath])
	assert.Equal(t, "0444", modes[agentproto.AuthorizedKeysPath])
	for _, c := range fake.CallsTo("ChownInContainer") {
		assert.Equal(t, "root", c.Args[2], "secrets are owned by the (root) agent")
	}
}

// When the agent never starts listening, the error must include its log so
// the cause (bad host key permissions, port in use, ...) is visible.
func TestLaunchTimeoutIncludesAgentLog(t *testing.T) {
	old := readyTimeout
	readyTimeout = 200 * time.Millisecond
	t.Cleanup(func() { readyTimeout = old })

	fake := &containertest.Fake{
		ExecFunc: func(_ string, argv []string) error {
			if isPing(argv) {
				return errors.New("connection refused")
			}
			return nil
		},
		Outputs: map[string]string{
			"tail -n 20 " + agentproto.LogPath: "dcx-agent: listen 0.0.0.0:48022: address already in use\n",
		},
	}
	t.Setenv("HOME", t.TempDir())

	err := launch(context.Background(), fake, agentOpts())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "address already in use")

	launch := strings.Join(fake.CallsTo("ExecDetachedAsRoot")[0].Args, " ")
	assert.Contains(t, launch, agentproto.LogPath, "agent output must be captured")

	// Root-owned dirs and the log are prepared as root first.
	setup := fake.CallsTo("ExecAsRoot")
	require.Len(t, setup, 1)
	assert.Equal(t, []string{"c1", agentproto.BinaryPath, "setup"}, setup[0].Args)
}

// notListeningUntilLaunched answers pings only after the agent was launched.
func notListeningUntilLaunched(fake *containertest.Fake) func(string, []string) error {
	return func(_ string, argv []string) error {
		if isPing(argv) && len(fake.CallsTo("ExecDetachedAsRoot")) == 0 {
			return errors.New("connection refused")
		}
		return nil
	}
}

func TestLaunchWithoutSessionUserUsesContainerDefaultUser(t *testing.T) {
	fake := &containertest.Fake{Outputs: map[string]string{strings.Join(defaultUIDQuery, " "): "1000\n"}}
	fake.ExecFunc = notListeningUntilLaunched(fake)
	t.Setenv("HOME", t.TempDir())

	opts := agentOpts()
	opts.User = ""
	require.NoError(t, launch(context.Background(), fake, opts))
	argv := strings.Join(fake.CallsTo("ExecDetachedAsRoot")[0].Args, " ")
	assert.Contains(t, argv, "--user 1000")
}

func TestLaunchHonoursListenAddr(t *testing.T) {
	var pinged []string
	fake := &containertest.Fake{}
	fake.ExecFunc = func(name string, argv []string) error {
		if isPing(argv) {
			pinged = append(pinged, argv[len(argv)-1])
		}
		return notListeningUntilLaunched(fake)(name, argv)
	}
	t.Setenv("HOME", t.TempDir())

	opts := agentOpts()
	opts.ListenAddr = "127.0.0.1:40123"
	require.NoError(t, launch(context.Background(), fake, opts))
	argv := strings.Join(fake.CallsTo("ExecDetachedAsRoot")[0].Args, " ")
	assert.Contains(t, argv, "--addr 127.0.0.1:40123")
	require.NotEmpty(t, pinged)
	for _, addr := range pinged {
		assert.Equal(t, "127.0.0.1:40123", addr, "probes must use the agent's port")
	}
}
