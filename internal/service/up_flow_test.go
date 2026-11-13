package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/agentctl"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/lifecycle"
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/ssh/hostconfig"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeHooks records which lifecycle phases Up ran.
type fakeHooks struct {
	initialize, create, start int
	failCreate                error
}

func (h *fakeHooks) RunInitialize(context.Context) error { h.initialize++; return nil }
func (h *fakeHooks) RunCreateHooks(ctx context.Context, m lifecycle.Markers) error {
	h.create++
	if m != nil { // like the real runner
		if _, err := m.Done(ctx, lifecycle.PhaseOnCreate); err != nil {
			return err
		}
	}
	if h.failCreate != nil {
		err := h.failCreate
		h.failCreate = nil
		return err
	}
	return nil
}
func (h *fakeHooks) RunStartHooks(context.Context) error { h.start++; return nil }

// upHarness is a workspace with an image config, a fake engine whose
// containers keep marker files, and fakes for the agent and the hooks.
type upHarness struct {
	t         *testing.T
	workspace string
	engine    *containertest.Fake
	hooks     *fakeHooks
	agents    int
}

func newUpHarness(t *testing.T, config string) *upHarness {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(hostconfig.SSHDirEnv, t.TempDir())
	h := &upHarness{t: t, workspace: t.TempDir(), hooks: &fakeHooks{}}
	h.writeConfig(config)

	h.engine = &containertest.Fake{Registry: map[string]map[string]string{"alpine:3": {}}}
	h.engine.FileMarkers() // marker files exist once written
	return h
}

func (h *upHarness) writeConfig(config string) {
	dir := filepath.Join(h.workspace, ".devcontainer")
	require.NoError(h.t, os.MkdirAll(dir, 0o755))
	require.NoError(h.t, os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(config), 0o644))
}

func (h *upHarness) up(opts UpOptions) error {
	svc := NewDevContainerService(h.engine, report.Discard, h.workspace, "", false)
	svc.provisionAgent = func(context.Context, container.Engine, report.Reporter, agentctl.Options) error {
		h.agents++
		return nil
	}
	svc.newHooks = func(*devcontainer.ResolvedDevContainer, int) lifecycleHooks { return h.hooks }
	return svc.Up(context.Background(), opts)
}

func (h *upHarness) containers() []state.ContainerSummary {
	l, err := h.engine.ListContainersWithLabels(context.Background(), map[string]string{state.LabelManaged: "true"})
	require.NoError(h.t, err)
	return l
}

const upTestConfig = `{"name": "up-test", "image": "alpine:3"}`

func TestUpCreatesAndCompletesSetup(t *testing.T) {
	h := newUpHarness(t, upTestConfig)
	require.NoError(t, h.up(UpOptions{}))

	require.Len(t, h.containers(), 1)
	assert.True(t, h.containers()[0].Running)
	assert.Equal(t, 1, h.agents)
	assert.Equal(t, fakeHooks{initialize: 1, create: 1, start: 1}, *h.hooks)
	id := h.containers()[0].ID
	assert.Contains(t, h.engine.Files, id+":"+markerPath(phaseReady), "setup completion is recorded")

	// Up again: nothing to do.
	require.NoError(t, h.up(UpOptions{}))
	assert.Len(t, h.engine.CallsTo("CreateContainer"), 1)
	assert.Equal(t, fakeHooks{initialize: 1, create: 1, start: 1}, *h.hooks, "a ready environment runs no hooks")
}

func TestUpResumesAfterAFailedHook(t *testing.T) {
	h := newUpHarness(t, upTestConfig)
	h.hooks.failCreate = errors.New("postCreateCommand exited with code 1")
	require.Error(t, h.up(UpOptions{}))
	require.Len(t, h.containers(), 1)
	id := h.containers()[0].ID
	assert.NotContains(t, h.engine.Files, id+":"+markerPath(phaseReady))

	require.NoError(t, h.up(UpOptions{}))
	assert.Len(t, h.engine.CallsTo("CreateContainer"), 1, "the container is reused")
	assert.Equal(t, 2, h.hooks.create, "create hooks are retried")
	assert.Equal(t, 1, h.hooks.start, "postStart runs once the first setup completes")
	assert.Contains(t, h.engine.Files, id+":"+markerPath(phaseReady))
}

func TestUpStartsAStoppedContainer(t *testing.T) {
	h := newUpHarness(t, upTestConfig)
	require.NoError(t, h.up(UpOptions{}))
	id := h.containers()[0].ID
	require.NoError(t, h.engine.StopContainer(context.Background(), id, nil))

	require.NoError(t, h.up(UpOptions{}))
	assert.Len(t, h.engine.CallsTo("CreateContainer"), 1)
	assert.Len(t, h.engine.CallsTo("StartContainer"), 2)
	assert.Equal(t, 2, h.agents, "the agent is relaunched after a start")
	assert.Equal(t, 2, h.hooks.start, "postStart runs on every start")
	assert.True(t, h.containers()[0].Running)
}

func TestUpRecreatesAStaleContainer(t *testing.T) {
	h := newUpHarness(t, upTestConfig)
	require.NoError(t, h.up(UpOptions{}))
	oldID := h.containers()[0].ID

	h.writeConfig(`{"name": "up-test", "image": "alpine:3", "containerEnv": {"CHANGED": "1"}}`)
	require.NoError(t, h.up(UpOptions{}))

	require.Len(t, h.containers(), 1)
	assert.NotEqual(t, oldID, h.containers()[0].ID, "a config change recreates the container")
	assert.Len(t, h.engine.CallsTo("RemoveContainer"), 1)
	assert.Equal(t, 2, h.hooks.create)
}

func TestUpResumesAPreMarkerContainerWhoseAgentIsDown(t *testing.T) {
	// Containers from before lifecycle markers have no marker label. When
	// their agent is gone, Up resumes with nil markers (it once passed a
	// typed nil and the hook runner dereferenced it).
	h := newUpHarness(t, upTestConfig)
	require.NoError(t, h.up(UpOptions{}))
	labels := state.ContainerLabelsFromMap(h.engine.Containers[0].Labels)
	labels.LifecycleMarkers = false
	h.engine.Containers[0].Labels = labels.ToMap()
	h.engine.ExecFunc = func(name string, argv []string) error {
		if len(argv) > 1 && argv[1] == "ping" {
			return errors.New("connection refused")
		}
		return nil
	}

	require.NoError(t, h.up(UpOptions{}))
	assert.Equal(t, 2, h.agents, "the agent is relaunched")
	assert.Equal(t, 2, h.hooks.create)
}

// A second Up on the same workspace waits for the first (R20) instead of
// racing it to create the container.
func TestUpWaitsForTheWorkspaceLock(t *testing.T) {
	h := newUpHarness(t, upTestConfig)
	svc := NewDevContainerService(h.engine, report.Discard, h.workspace, "", false)
	unlock, err := svc.lockWorkspace(context.Background(), svc.Identity().WorkspaceID)
	require.NoError(t, err)

	done := make(chan error, 1)
	go func() { done <- h.up(UpOptions{}) }()
	select {
	case err := <-done:
		t.Fatalf("Up ran while the workspace was locked: %v", err)
	case <-time.After(500 * time.Millisecond):
	}
	assert.Empty(t, h.engine.CallsTo("CreateContainer"))

	unlock()
	require.NoError(t, <-done)
	assert.Len(t, h.engine.CallsTo("CreateContainer"), 1)
}
