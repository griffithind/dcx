package service

import (
	"context"
	"os"
	"path/filepath"
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

// statusContainer is the workspace's primary container recorded with the
// given hashes (at the current hash algorithm).
func statusContainer(t *testing.T, svc *DevContainerService, configHash, inputsHash string) state.ContainerSummary {
	t.Helper()
	l := state.NewContainerLabels()
	l.WorkspaceID, l.IsPrimary = svc.Identity().WorkspaceID, true
	l.BuildMethod = state.BuildMethodImage
	l.HashConfig, l.HashInputs, l.HashVersion = configHash, inputsHash, state.CurrentHashVersion
	return state.ContainerSummary{ID: "c1", Name: "svc-test-c1", State: "running", Running: true, Labels: l.ToMap()}
}

func TestStatusAbsent(t *testing.T) {
	svc, _ := newTestService(t, &containertest.Fake{})
	r, err := svc.Status(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, state.StateAbsent, r.State)
	assert.Nil(t, r.Container)
	assert.NotEmpty(t, r.ConfigHash)
	assert.Empty(t, r.ConfigError)
	assert.Equal(t, "alpine:latest", r.BaseImage)
	assert.Empty(t, r.SSHAlias, "no SSH alias before the environment exists")
}

func TestStatusOverContainerStates(t *testing.T) {
	fake := &containertest.Fake{StartedAt: map[string]string{"c1": "2026-01-01T00:00:00Z"}}
	svc, _ := newTestService(t, fake)
	resolved, err := svc.Load(context.Background())
	require.NoError(t, err)

	for _, tc := range []struct {
		name               string
		configHash, inputs string
		want               state.ContainerState
		drift              bool
	}{
		{"current", resolved.ConfigHash, resolved.InputsHash, state.StateRunning, false},
		{"drift: same files, other inputs", resolved.ConfigHash, "older-inputs", state.StateRunning, true},
		{"stale: files changed", "older-config", "older-inputs", state.StateStale, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake.Containers = []state.ContainerSummary{statusContainer(t, svc, tc.configHash, tc.inputs)}
			r, err := svc.Status(context.Background(), true)
			require.NoError(t, err)
			assert.Equal(t, tc.want, r.State)
			assert.Equal(t, tc.drift, r.Drift)
			require.NotNil(t, r.Container)
			assert.Equal(t, "c1", r.Container.ID)
			assert.Equal(t, "2026-01-01T00:00:00Z", r.StartedAt, "detailed output inspects the container")
		})
	}
}

func TestStatusFallsBackToTheRawConfig(t *testing.T) {
	fake := &containertest.Fake{}
	svc, _ := newTestService(t, fake)
	fake.Containers = []state.ContainerSummary{statusContainer(t, svc, "some-hash", "")}
	// A config that parses but does not resolve (two plans), with shortcuts.
	writeConfig(t, svc, `{"image": "alpine:3", "dockerComposeFile": "c.yml", "service": "app",
		"customizations": {"dcx": {"shortcuts": {"a": "echo a", "b": "echo b"}}}}`)

	r, err := svc.Status(context.Background(), false)
	require.NoError(t, err)
	assert.Contains(t, r.ConfigError, "dockerComposeFile cannot be combined with image or build")
	assert.Empty(t, r.ConfigHash, "staleness is not judged without a resolved config")
	assert.Equal(t, state.StateRunning, r.State, "the container state is still reported")
	require.NotNil(t, r.Config)
	assert.Equal(t, 2, r.Shortcuts, "shortcuts come from the raw file")
}

// writeConfig replaces the test service's devcontainer.json.
func writeConfig(t *testing.T, svc *DevContainerService, config string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(svc.workspacePath, ".devcontainer", "devcontainer.json"), []byte(config), 0o644))
}
