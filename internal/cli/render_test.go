package cli

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

// captureUI renders through the ui package into a buffer, without colour.
func captureUI(t *testing.T, render func()) string {
	t.Helper()
	var buf bytes.Buffer
	ui.Configure(ui.Config{Verbosity: ui.VerbosityNormal, NoColor: true, Writer: &buf, ErrWriter: &buf})
	t.Cleanup(func() { ui.Configure(ui.Config{Verbosity: ui.VerbosityNormal}) })
	render()
	return buf.String()
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "missing golden file; run: go test ./internal/cli -run %s -args -update", t.Name())
	require.Equal(t, string(want), got)
}

func runningReport() *service.StatusReport {
	labels := state.NewContainerLabels()
	labels.WorkspaceID = "abcd1234efgh"
	labels.WorkspaceName = "myapp"
	labels.HashConfig = "0123456789abcdef0123"
	labels.BuildMethod = state.BuildMethodImage
	labels.IsPrimary = true
	return &service.StatusReport{
		WorkspacePath: "/home/dev/myapp",
		Identity:      &devcontainer.Identity{ResourceName: "myapp-abcd1234", WorkspaceID: "abcd1234efgh", SSHHost: "myapp.dcx"},
		State:         state.StateRunning,
		Container: &state.ContainerInfo{
			ID: "f00dfeedbeef0123456789", Name: "myapp-abcd1234", Status: "running", Running: true,
			ConfigHash: "0123456789abcdef0123", Labels: labels,
		},
		SSHAlias: "myapp.dcx",
		Config: &devcontainer.DevContainerConfig{
			Name: "myapp", Image: "mcr.microsoft.com/devcontainers/go:1", RemoteUser: "vscode",
			WorkspaceFolder: "/workspaces/myapp",
			Features:        map[string]interface{}{"ghcr.io/devcontainers/features/node:1": nil, "ghcr.io/devcontainers/features/docker-in-docker:2": nil},
		},
		ConfigHash: "0123456789abcdef0123",
		Shortcuts:  2,
		PlanType:   devcontainer.PlanTypeImage,
		BaseImage:  "mcr.microsoft.com/devcontainers/go:1",
		Features:   []string{"ghcr.io/devcontainers/features/docker-in-docker:2", "ghcr.io/devcontainers/features/node:1"},
		Image:      "dcx/abcd1234efgh:uid-0123",
		StartedAt:  "2026-11-13T10:00:00Z",
	}
}

func TestRenderStatus(t *testing.T) {
	golden(t, "status_running", captureUI(t, func() { renderStatus(runningReport(), false) }))
	golden(t, "status_running_detailed", captureUI(t, func() { renderStatus(runningReport(), true) }))

	absent := &service.StatusReport{
		WorkspacePath: "/home/dev/empty",
		Identity:      &devcontainer.Identity{WorkspaceID: "zzzz9999yyyy"},
		State:         state.StateAbsent,
	}
	golden(t, "status_absent", captureUI(t, func() { renderStatus(absent, true) }))

	stale := runningReport()
	stale.State, stale.ConfigHash = state.StateStale, "fedcba9876543210fedc"
	golden(t, "status_stale", captureUI(t, func() { renderStatus(stale, false) }))

	drift := runningReport()
	drift.Drift = true
	golden(t, "status_drift", captureUI(t, func() { renderStatus(drift, false) }))

	broken := &service.StatusReport{
		WorkspacePath: "/home/dev/broken",
		Identity:      &devcontainer.Identity{WorkspaceID: "bbbb1111cccc"},
		State:         state.StateAbsent,
		ConfigError:   "failed to load configuration: invalid character '}' looking for beginning of object key string",
	}
	golden(t, "status_config_error", captureUI(t, func() { renderStatus(broken, false) }))
}

func TestRenderList(t *testing.T) {
	created := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	envs := []service.Environment{
		{
			WorkspaceID: "abcd1234efgh", Name: "myapp", WorkspacePath: "/home/dev/myapp", State: state.StateRunning,
			Containers: []service.EnvironmentContainer{
				{ID: "c1", Name: "myapp-abcd1234-app-1", Status: "running", IsPrimary: true, CreatedAt: created},
				{ID: "c2", Name: "myapp-abcd1234-db-1", Status: "running", CreatedAt: created},
			},
		},
		{
			WorkspaceID: "nonamexxxxxx", WorkspacePath: "/home/dev/a/very/long/path/that/goes/on/and/on/until/it/is/truncated",
			State:      state.StateCreated,
			Containers: []service.EnvironmentContainer{{ID: "c3", Name: "noname-nonamexx", Status: "exited", IsPrimary: true}},
		},
	}
	golden(t, "list", captureUI(t, func() { require.NoError(t, renderList(envs, true)) }))
	golden(t, "list_empty", captureUI(t, func() { require.NoError(t, renderList(nil, false)) }))
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	require.NoError(t, err)
	return string(data) + "\n"
}

// The --json outputs are a contract for scripts and tests; golden files
// make any change to them deliberate.
func TestStatusAndListJSON(t *testing.T) {
	golden(t, "status_running.json", marshal(t, statusToJSON(runningReport(), false)))
	detailed := statusToJSON(runningReport(), true)
	detailed.Detail.DCXVersion, detailed.Detail.Platform = "VERSION", "OS/ARCH"
	golden(t, "status_detailed.json", marshal(t, detailed))
	golden(t, "status_absent.json", marshal(t, statusToJSON(&service.StatusReport{
		WorkspacePath: "/home/dev/empty",
		Identity:      &devcontainer.Identity{WorkspaceID: "zzzz9999yyyy", Name: "empty", ResourceName: "empty-zzzz9999"},
		State:         state.StateAbsent,
	}, false)))
	created := time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	golden(t, "list.json", marshal(t, listToJSON([]service.Environment{{
		WorkspaceID: "abcd1234efgh", Name: "myapp", WorkspacePath: "/home/dev/myapp", State: state.StateRunning,
		BuildMethod: "compose", CreatedAt: created,
		Containers: []service.EnvironmentContainer{{ID: "c1", Name: "myapp-app-1", Status: "running", IsPrimary: true, CreatedAt: created}},
	}})))
}
