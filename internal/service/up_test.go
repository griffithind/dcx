package service

import (
	"context"
	"errors"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func markerInfo(withMarkers bool) *state.ContainerInfo {
	return &state.ContainerInfo{ID: "c1", Name: "c1", Running: true, Labels: &state.ContainerLabels{LifecycleMarkers: withMarkers}}
}

func TestContainerMarkersRoundTrip(t *testing.T) {
	written := map[string]bool{}
	fake := &containertest.Fake{}
	fake.ExecFunc = func(_ string, argv []string) error {
		if len(argv) == 3 && argv[0] == "test" && written[argv[2]] {
			return nil
		}
		return errors.New("missing")
	}
	svc, _ := newTestService(t, fake)
	m := svc.markersFor(markerInfo(true))
	require.NotNil(t, m)

	ctx := context.Background()
	assert.False(t, m.Done(ctx, phaseReady))
	require.NoError(t, m.Mark(ctx, phaseReady))
	for k := range fake.Files {
		written[k[len("c1:"):]] = true
	}
	assert.True(t, m.Done(ctx, phaseReady))
	assert.Contains(t, fake.Files, "c1:"+lifecycleMarkerDir+"/ready.done")

	mkdirs := fake.CallsTo("MkdirInContainer")
	require.NotEmpty(t, mkdirs)
	assert.Equal(t, []string{"c1", lifecycleMarkerDir, "root"}, mkdirs[0].Args)
}

// A running container whose setup never finished must not be reported as
// "already running"; one created before markers existed must.
func TestSetupComplete(t *testing.T) {
	fake := &containertest.Fake{ExecFunc: func(string, []string) error { return errors.New("missing") }}
	svc, _ := newTestService(t, fake)
	ctx := context.Background()

	assert.True(t, svc.setupComplete(ctx, markerInfo(false)), "legacy containers are assumed complete")
	assert.False(t, svc.setupComplete(ctx, markerInfo(true)), "no ready marker: setup unfinished")

	fake.ExecFunc = nil // every `test -f` succeeds
	assert.True(t, svc.setupComplete(ctx, markerInfo(true)))
}
