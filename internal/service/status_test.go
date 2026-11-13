package service

import (
	"context"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func managed(id, ws, name, path string, running, primary bool, created time.Time) state.ContainerSummary {
	l := state.NewContainerLabels()
	l.WorkspaceID, l.WorkspaceName, l.WorkspacePath, l.IsPrimary = ws, name, path, primary
	l.BuildMethod = state.BuildMethodCompose
	st := "exited"
	if running {
		st = "running"
	}
	return state.ContainerSummary{ID: id, Name: id, State: st, Running: running, Labels: l.ToMap(), Created: created}
}

func TestListGroupsByWorkspace(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fake := &containertest.Fake{Containers: []state.ContainerSummary{
		managed("b-app", "wsB", "b", "/z/b", true, true, t0.Add(time.Hour)),
		managed("b-db", "wsB", "b", "/z/b", true, false, t0),
		managed("a-app", "wsA", "a", "/a/a", false, true, t0),
	}}
	svc, _ := newTestService(t, fake)

	envs, err := svc.List(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, envs, 2)
	assert.Equal(t, "/a/a", envs[0].WorkspacePath, "sorted by workspace path")
	assert.Equal(t, "wsB", envs[1].WorkspaceID)
	assert.Len(t, envs[1].Containers, 2)
	assert.Equal(t, t0, envs[1].CreatedAt, "an environment is as old as its oldest container")
	assert.Equal(t, state.StateRunning, envs[1].State)

	running, err := svc.List(context.Background(), false)
	require.NoError(t, err)
	require.Len(t, running, 1, "without --all, stopped environments are left out")
	assert.Equal(t, "wsB", running[0].WorkspaceID)
}
