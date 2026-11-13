package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/secrets"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestService returns a service over a fake engine with an isolated HOME
// and a minimal workspace.
func newTestService(t *testing.T, fake *containertest.Fake) (*DevContainerService, *report.Recorder) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".devcontainer"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".devcontainer", "devcontainer.json"),
		[]byte(`{"name": "svc-test", "image": "alpine:latest"}`), 0o644))

	rec := &report.Recorder{}
	return NewDevContainerService(fake, rec, ws, "", false), rec
}

func singleContainer(id, workspaceID string, running bool) state.ContainerSummary {
	st := "exited"
	if running {
		st = "running"
	}
	return state.ContainerSummary{
		ID: id, Name: id, State: st, Running: running,
		Labels: map[string]string{
			state.LabelWorkspaceID: workspaceID,
			state.LabelIsPrimary:   "true",
			state.LabelBuildMethod: state.BuildMethodImage,
		},
	}
}

func isPing(argv []string) bool {
	return len(argv) >= 2 && argv[0] == agentproto.BinaryPath && argv[1] == "ping"
}

func TestDownWithIDsStopsAndRemovesSingleContainer(t *testing.T) {
	fake := &containertest.Fake{Containers: []state.ContainerSummary{singleContainer("c1", "ws1", true)}}
	svc, _ := newTestService(t, fake)

	require.NoError(t, svc.DownWithIDs(context.Background(), "", "ws1", DownOptions{}))

	require.Len(t, fake.CallsTo("StopContainer"), 1)
	removes := fake.CallsTo("RemoveContainer")
	require.Len(t, removes, 1)
	// `docker rm -v` only drops anonymous volumes; named volumes are never
	// touched for single containers.
	assert.Equal(t, []string{"c1", "force=true", "volumes=true"}, removes[0].Args)
	assert.Empty(t, fake.Containers)
}

func TestDownWithIDsSkipsStopForStoppedContainer(t *testing.T) {
	fake := &containertest.Fake{Containers: []state.ContainerSummary{singleContainer("c1", "ws1", false)}}
	svc, _ := newTestService(t, fake)

	require.NoError(t, svc.DownWithIDs(context.Background(), "", "ws1", DownOptions{}))
	assert.Empty(t, fake.CallsTo("StopContainer"))
	assert.Len(t, fake.CallsTo("RemoveContainer"), 1)
}

func TestDownWithIDsAbsentIsNoop(t *testing.T) {
	fake := &containertest.Fake{}
	svc, rec := newTestService(t, fake)

	require.NoError(t, svc.DownWithIDs(context.Background(), "", "ws1", DownOptions{}))
	assert.Empty(t, fake.CallsTo("RemoveContainer"))
	require.NotEmpty(t, rec.Messages())
	assert.Equal(t, "No devcontainer found", rec.Messages()[0].Text)
}

func agentResolved() *devcontainer.ResolvedDevContainer {
	return &devcontainer.ResolvedDevContainer{EffectiveUser: "dev", WorkspaceFolder: "/workspaces/app"}
}

func TestLaunchSSHAgentSkipsWhenAlreadyListening(t *testing.T) {
	fake := &containertest.Fake{}
	svc, _ := newTestService(t, fake)

	err := svc.launchSSHAgent(context.Background(), agentResolved(), &state.ContainerInfo{Name: "c1"}, nil)
	require.NoError(t, err)
	assert.Empty(t, fake.CallsTo("ExecDetached"))
}

func TestLaunchSSHAgentStartsAndWaitsForReadiness(t *testing.T) {
	var pings atomic.Int32
	fake := &containertest.Fake{ExecFunc: func(_ string, argv []string) error {
		// Not listening on the idempotency probe and the first two readiness
		// polls; listening afterwards.
		if isPing(argv) && pings.Add(1) <= 3 {
			return errors.New("connection refused")
		}
		return nil
	}}
	svc, _ := newTestService(t, fake)

	err := svc.launchSSHAgent(context.Background(), agentResolved(), &state.ContainerInfo{Name: "c1"}, []string{"10.0.0.0/24"})
	require.NoError(t, err)

	launches := fake.CallsTo("ExecDetached")
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

func TestLaunchSSHAgentFailsWhenAgentNeverListens(t *testing.T) {
	old := agentReadyTimeout
	agentReadyTimeout = 300 * time.Millisecond
	t.Cleanup(func() { agentReadyTimeout = old })

	fake := &containertest.Fake{ExecFunc: func(_ string, argv []string) error {
		if isPing(argv) {
			return errors.New("connection refused")
		}
		return nil
	}}
	svc, _ := newTestService(t, fake)

	err := svc.launchSSHAgent(context.Background(), agentResolved(), &state.ContainerInfo{Name: "c1"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not start listening")
}

func TestMountSSHSecretsWritesKeysWithOwnerAndModes(t *testing.T) {
	fake := &containertest.Fake{}
	svc, _ := newTestService(t, fake)

	err := svc.mountSSHSecrets(context.Background(), agentResolved(), &state.ContainerInfo{Name: "c1", Labels: &state.ContainerLabels{}})
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
		assert.Equal(t, "dev", c.Args[2], "secrets are owned by the agent's user")
	}
}

func TestMountSecretsRejectsUnsafeNameWithoutWriting(t *testing.T) {
	fake := &containertest.Fake{}
	err := container.MountSecretsToContainer(context.Background(), fake, "c1",
		[]secrets.Secret{{Name: "../../etc/sudoers.d/x", Value: []byte("x")}}, "dev")
	require.Error(t, err)
	assert.Empty(t, fake.CallsTo("WriteFileInContainer"))
}
