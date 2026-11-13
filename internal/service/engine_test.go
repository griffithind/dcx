package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestDownWithIDsStopsAndRemovesSingleContainer(t *testing.T) {
	fake := &containertest.Fake{Containers: []state.ContainerSummary{singleContainer("c1", "ws1", true)}}
	svc, _ := newTestService(t, fake)

	require.NoError(t, svc.DownWithIDs(context.Background(), "", "ws1", DownOptions{}))

	require.Len(t, fake.CallsTo("StopContainer"), 1)
	removes := fake.CallsTo("RemoveContainer")
	require.Len(t, removes, 1)
	// `docker rm -v` only drops anonymous volumes; without --volumes the
	// named ones are kept.
	assert.Equal(t, []string{"c1", "force=true", "volumes=true"}, removes[0].Args)
	assert.Empty(t, fake.Containers)
	assert.Empty(t, fake.CallsTo("RemoveVolume"))
}

// `down --volumes` removes a single container's named volumes too (F18),
// as `compose down -v` does.
func TestDownWithVolumesRemovesNamedVolumes(t *testing.T) {
	fake := &containertest.Fake{
		Containers: []state.ContainerSummary{singleContainer("c1", "ws1", false)},
		Volumes:    map[string][]string{"c1": {"app-data", "cache"}},
	}
	svc, _ := newTestService(t, fake)
	require.NoError(t, svc.DownWithIDs(context.Background(), "", "ws1", DownOptions{RemoveVolumes: true}))
	var removed []string
	for _, c := range fake.CallsTo("RemoveVolume") {
		removed = append(removed, c.Args[0])
	}
	assert.Equal(t, []string{"app-data", "cache"}, removed)
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

func TestMountSecretsRejectsUnsafeNameWithoutWriting(t *testing.T) {
	fake := &containertest.Fake{}
	err := container.MountSecretsToContainer(context.Background(), fake, "c1",
		[]secrets.Secret{{Name: "../../etc/sudoers.d/x", Value: []byte("x")}}, "dev")
	require.Error(t, err)
	assert.Empty(t, fake.CallsTo("WriteFileInContainer"))
}

func TestLoadPullsImageForMetadataWhenRequested(t *testing.T) {
	fake := &containertest.Fake{Registry: map[string]map[string]string{
		"alpine:latest": {devcontainer.DevcontainerMetadataLabel: `[{"remoteUser": "vscode"}]`},
	}}
	svc, _ := newTestService(t, fake)

	// Without PullForMetadata a missing image is resolved without metadata.
	resolved, err := svc.LoadWithOptions(context.Background(), LoadOptions{})
	require.NoError(t, err)
	assert.Equal(t, "", resolved.RemoteUser)
	assert.Empty(t, fake.CallsTo("PullImageWithProgress"))

	// With it (as `up` and `build` do) the image is pulled and its
	// metadata merged, so the first run resolves like later ones.
	resolved, err = svc.LoadWithOptions(context.Background(), LoadOptions{PullForMetadata: true})
	require.NoError(t, err)
	assert.Equal(t, "vscode", resolved.RemoteUser)
	assert.Len(t, fake.CallsTo("PullImageWithProgress"), 1)
}

func TestLoadSurvivesFailedMetadataPull(t *testing.T) {
	fake := &containertest.Fake{}
	svc, rec := newTestService(t, fake)

	resolved, err := svc.LoadWithOptions(context.Background(), LoadOptions{PullForMetadata: true})
	require.NoError(t, err)
	assert.Equal(t, "", resolved.RemoteUser)
	var warned bool
	for _, m := range rec.Messages() {
		if m.Level == report.LevelWarn && strings.Contains(m.Text, "Could not pull") {
			warned = true
		}
	}
	assert.True(t, warned, "a failed pull should be reported, not fatal")
}
