package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/dcxhome"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dcx clean removes what dcx keeps for workspaces without a container
// (folders and older-layout files), keeps those with one, and skips a
// workspace whose lock a dcx command holds.
func TestPruneWorkspaces(t *testing.T) {
	home := t.TempDir()
	t.Setenv(dcxhome.Env, home)
	fake := &containertest.Fake{Containers: []state.ContainerSummary{
		{ID: "c1", Labels: map[string]string{state.LabelManaged: "true", state.LabelWorkspaceID: "alive"}},
	}}
	svc, _ := newTestService(t, fake)
	t.Setenv(dcxhome.Env, home) // newTestService points it elsewhere

	write := func(rel string) {
		p := filepath.Join(home, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o700))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	}
	for _, id := range []string{"alive", "gone", "busy"} {
		write("workspaces/" + id + "/host_key")
	}
	write("hostkeys/alive.key")
	write("hostkeys/old1.key")
	write("hostkeys/old2.key")
	write("compose/old1/docker-compose.dcx.yml")
	write("locks/old1.lock")

	unlock, ok, err := tryLockWorkspace("busy")
	require.NoError(t, err)
	require.True(t, ok)
	defer unlock()

	dry, err := svc.PruneWorkspaces(context.Background(), true)
	require.NoError(t, err)
	assert.Equal(t, WorkspacePrune{Folders: 2, Legacy: 4}, *dry, "a dry run counts, busy included")
	assert.FileExists(t, filepath.Join(home, "workspaces", "gone", "host_key"), "a dry run removes nothing")

	got, err := svc.PruneWorkspaces(context.Background(), false)
	require.NoError(t, err)
	assert.Equal(t, WorkspacePrune{Folders: 1, Legacy: 4, Busy: 1}, *got)
	assert.FileExists(t, filepath.Join(home, "workspaces", "alive", "host_key"))
	assert.FileExists(t, filepath.Join(home, "workspaces", "busy", "host_key"))
	assert.NoDirExists(t, filepath.Join(home, "workspaces", "gone"))
	assert.FileExists(t, filepath.Join(home, "hostkeys", "alive.key"))
	assert.NoFileExists(t, filepath.Join(home, "hostkeys", "old1.key"))
	assert.NoDirExists(t, filepath.Join(home, "compose"), "emptied legacy folders go")
	assert.NoDirExists(t, filepath.Join(home, "locks"))
}
