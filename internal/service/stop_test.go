package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stopTestService returns a service whose workspace has one single-container
// devcontainer in the given running state.
func stopTestService(t *testing.T, running bool, shutdownAction string) (*DevContainerService, *containertest.Fake) {
	t.Helper()
	fake := &containertest.Fake{}
	svc, _ := newTestService(t, fake)
	if shutdownAction != "" {
		require.NoError(t, os.WriteFile(filepath.Join(svc.workspacePath, ".devcontainer", "devcontainer.json"),
			[]byte(`{"name": "svc-test", "image": "alpine:latest", "shutdownAction": "`+shutdownAction+`"}`), 0o644))
	}
	ids, err := svc.GetIdentifiers()
	require.NoError(t, err)
	labels := state.NewContainerLabels()
	labels.WorkspaceID = ids.WorkspaceID
	labels.BuildMethod = state.BuildMethodImage
	labels.IsPrimary = true
	status := "exited"
	if running {
		status = "running"
	}
	fake.Containers = []state.ContainerSummary{{ID: "c1", Name: "svc-test-1", State: status, Running: running, Labels: labels.ToMap()}}
	return svc, fake
}

func TestStop(t *testing.T) {
	ctx := context.Background()

	svc, fake := stopTestService(t, true, "")
	outcome, err := svc.Stop(ctx, StopOptions{})
	require.NoError(t, err)
	assert.Equal(t, StopDone, outcome)
	assert.Len(t, fake.CallsTo("StopContainer"), 1)

	svc, fake = stopTestService(t, false, "")
	outcome, err = svc.Stop(ctx, StopOptions{})
	require.NoError(t, err)
	assert.Equal(t, StopAlreadyStopped, outcome)
	assert.Empty(t, fake.CallsTo("StopContainer"))

	empty, _ := newTestService(t, &containertest.Fake{})
	outcome, err = empty.Stop(ctx, StopOptions{})
	require.NoError(t, err)
	assert.Equal(t, StopNotFound, outcome)
}

func TestStopHonoursShutdownActionNone(t *testing.T) {
	ctx := context.Background()
	svc, fake := stopTestService(t, true, "none")

	outcome, err := svc.Stop(ctx, StopOptions{})
	require.NoError(t, err)
	assert.Equal(t, StopSkipped, outcome)
	assert.Empty(t, fake.CallsTo("StopContainer"))

	outcome, err = svc.Stop(ctx, StopOptions{Force: true})
	require.NoError(t, err)
	assert.Equal(t, StopDone, outcome)
	assert.Len(t, fake.CallsTo("StopContainer"), 1)
}

func TestRestartSkipsWithShutdownActionNone(t *testing.T) {
	svc, fake := stopTestService(t, true, "none")
	skipped, err := svc.Restart(context.Background(), RestartOptions{})
	require.NoError(t, err)
	assert.True(t, skipped)
	assert.Empty(t, fake.CallsTo("StopContainer"))

	empty, _ := newTestService(t, &containertest.Fake{})
	_, err = empty.Restart(context.Background(), RestartOptions{})
	assert.ErrorContains(t, err, "no devcontainer found")
}
