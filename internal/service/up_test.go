package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func markerInfo(withMarkers bool) *state.ContainerInfo {
	return &state.ContainerInfo{ID: "c1", Name: "c1", Running: true, Labels: &state.ContainerLabels{LifecycleMarkers: withMarkers}}
}

func TestContainerMarkersRoundTrip(t *testing.T) {
	fake := &containertest.Fake{}
	fake.FileMarkers()
	svc, _ := newTestService(t, fake)
	m := svc.markersFor(markerInfo(true))
	require.NotNil(t, m)

	ctx := context.Background()
	done, err := m.Done(ctx, phaseReady)
	require.NoError(t, err)
	assert.False(t, done)
	require.NoError(t, m.Mark(ctx, phaseReady))
	done, err = m.Done(ctx, phaseReady)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Contains(t, fake.Files, "c1:"+lifecycleMarkerDir+"/ready.done")

	require.NoError(t, m.Mark(ctx, phasePostStart, phaseReady))
	assert.Len(t, fake.CallsTo("ExecAsRoot"), 2, "one exec marks any number of phases")
	assert.Contains(t, fake.Files, "c1:"+lifecycleMarkerDir+"/poststart.done")
	assert.Len(t, fake.CallsTo("ExecOutput"), 1, "the markers are listed once")
}

// A running container whose setup never finished must not be reported as
// "already running"; one created before markers existed must; and a
// container that cannot be asked is an error, not "unfinished".
func TestSetupComplete(t *testing.T) {
	fake := &containertest.Fake{}
	fake.FileMarkers()
	svc, _ := newTestService(t, fake)
	ctx := context.Background()

	done, err := svc.setupComplete(ctx, svc.markersFor(markerInfo(false)))
	require.NoError(t, err)
	assert.True(t, done, "legacy containers are assumed complete")

	done, err = svc.setupComplete(ctx, svc.markersFor(markerInfo(true)))
	require.NoError(t, err)
	assert.False(t, done, "no ready marker: setup unfinished")

	fake.Files = map[string][]byte{"c1:" + markerPath(phaseReady): []byte("x")}
	done, err = svc.setupComplete(ctx, svc.markersFor(markerInfo(true)))
	require.NoError(t, err)
	assert.True(t, done)

	fake.OutputFunc = func(string, []string) ([]byte, error) { return nil, errors.New("container not responding") }
	_, err = svc.setupComplete(ctx, svc.markersFor(markerInfo(true)))
	assert.Error(t, err)
}

func TestDecideUp(t *testing.T) {
	none, recreate, rebuild := UpOptions{}, UpOptions{Recreate: true}, UpOptions{Rebuild: true}
	for _, tt := range []struct {
		state    state.ContainerState
		complete bool
		opts     UpOptions
		want     upAction
	}{
		{state.StateAbsent, false, none, actionCreate},
		{state.StateAbsent, false, recreate, actionCreate},
		{state.StateRunning, true, none, actionNone},
		{state.StateRunning, false, none, actionResume},
		{state.StateRunning, true, recreate, actionRecreate},
		{state.StateRunning, false, rebuild, actionRecreate},
		{state.StateCreated, false, none, actionStart},
		{state.StateCreated, false, recreate, actionRecreate},
		{state.StateStale, true, none, actionRecreate},
		{state.StateBroken, false, none, actionRecreate},
	} {
		assert.Equal(t, tt.want, decideUp(tt.state, tt.complete, tt.opts), "%s complete=%v opts=%+v", tt.state, tt.complete, tt.opts)
	}
}

// A GPU request, optional or required, never adds --gpus on a host
// without a GPU runtime (it would fail the start); a required one warns
// (R6/C12).
func TestGPUDroppedWithoutRuntime(t *testing.T) {
	for _, mode := range []devcontainer.GPUMode{devcontainer.GPUOptional, devcontainer.GPURequired} {
		svc, rec := newTestService(t, &containertest.Fake{}) // fake Info reports no GPU
		resolved := &devcontainer.ResolvedDevContainer{
			HostRequirements: &devcontainer.HostRequirements{GPU: &devcontainer.GPURequirement{Mode: mode}},
			GPU:              &devcontainer.GPURequirement{Mode: mode},
		}
		svc.checkHostRequirements(context.Background(), resolved)
		assert.Nil(t, resolved.GPU, "mode %d", mode)
		assert.Equal(t, mode == devcontainer.GPURequired, strings.Contains(fmt.Sprint(rec.Messages()), "no GPU runtime"), "mode %d", mode)
	}
}

func TestSSHAccessIsKeptUnlessChangedWithACreate(t *testing.T) {
	widened := container.SSHAccess{AllowedCIDRs: []string{"10.0.0.0/24"}}
	labels := state.NewContainerLabels()
	labels.SSHAllowedCIDRs = widened.AllowedCIDRs
	existing := &state.ContainerInfo{Labels: labels}
	loopback := container.SSHAccess{}

	// No --hosts: keep what the container records (restart, plain up).
	got, err := sshAccessFor(actionStart, existing, nil)
	require.NoError(t, err)
	assert.Equal(t, widened, got)
	got, err = sshAccessFor(actionRecreate, existing, nil)
	require.NoError(t, err)
	assert.Equal(t, widened, got, "a recreate keeps --hosts too")

	// New container without --hosts: loopback.
	got, err = sshAccessFor(actionCreate, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, loopback, got)

	// --hosts with a (re)create applies.
	got, err = sshAccessFor(actionRecreate, existing, &loopback)
	require.NoError(t, err)
	assert.Equal(t, loopback, got)

	// The same --hosts again is fine; a different one needs --recreate.
	_, err = sshAccessFor(actionNone, existing, &widened)
	require.NoError(t, err)
	_, err = sshAccessFor(actionStart, existing, &loopback)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--recreate")
}
