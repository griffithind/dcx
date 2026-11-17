package devcontainer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The git repository containing the workspace is mounted, and the default
// workspaceFolder is the workspace's place in it (C2).
func TestWorkspaceInGitRepositoryMountsTheRoot(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "mono")
	ws := filepath.Join(repo, "services", "api")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o755))
	require.NoError(t, os.MkdirAll(ws, 0o755))
	cfg := &DevContainerConfig{Image: "alpine"}

	assert.Equal(t, "/workspaces/mono/services/api", DetermineContainerWorkspaceFolder(cfg, ws))
	assert.Equal(t, &Mount{Type: "bind", Source: repo, Target: "/workspaces/mono"}, resolveWorkspaceMount(cfg, ws))

	// The repository root itself, and a worktree's .git file.
	assert.Equal(t, "/workspaces/mono", DetermineContainerWorkspaceFolder(cfg, repo))
	wt := filepath.Join(t.TempDir(), "wt")
	require.NoError(t, os.MkdirAll(wt, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /x/.git/worktrees/wt\n"), 0o644))
	assert.Equal(t, &Mount{Type: "bind", Source: wt, Target: "/workspaces/wt"}, resolveWorkspaceMount(cfg, wt))
}

// workspaceFolder moves the working directory, not the mount (C3).
func TestWorkspaceFolderDoesNotMoveTheMount(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "proj")
	require.NoError(t, os.MkdirAll(ws, 0o755))
	cfg := &DevContainerConfig{Image: "alpine", WorkspaceFolder: "/app"}

	assert.Equal(t, "/app", DetermineContainerWorkspaceFolder(cfg, ws))
	assert.Equal(t, &Mount{Type: "bind", Source: ws, Target: "/workspaces/proj"}, resolveWorkspaceMount(cfg, ws))

	empty := ""
	assert.Nil(t, resolveWorkspaceMount(&DevContainerConfig{Image: "alpine", WorkspaceMount: &empty}, ws))
	assert.Nil(t, resolveWorkspaceMount(&DevContainerConfig{DockerComposeFile: StringOrSlice{"c.yml"}, Service: "app"}, ws))
	custom := "type=bind,source=/src,target=/code"
	assert.Equal(t, &Mount{Type: "bind", Source: "/src", Target: "/code"}, resolveWorkspaceMount(&DevContainerConfig{Image: "alpine", WorkspaceMount: &custom}, ws))
}

// A layout that is not the plain bind of the workspace at the workspace
// folder is part of the inputs hash, so containers created with the old
// layout report drift.
func TestWorkspaceLayoutIsAnInput(t *testing.T) {
	ws := "/home/u/proj"
	plain := &ResolvedDevContainer{LocalRoot: ws, WorkspaceFolder: "/workspaces/proj", Local: &DevContainerConfig{},
		WorkspaceMount: &Mount{Type: "bind", Source: ws, Target: "/workspaces/proj"}}
	assert.Equal(t, "h", workspaceInputsHash("h", plain))

	gitRoot := *plain
	gitRoot.WorkspaceFolder = "/workspaces/u/proj"
	gitRoot.WorkspaceMount = &Mount{Type: "bind", Source: "/home/u", Target: "/workspaces/u"}
	assert.NotEqual(t, "h", workspaceInputsHash("h", &gitRoot))

	movedFolder := *plain
	movedFolder.WorkspaceFolder = "/app"
	assert.NotEqual(t, "h", workspaceInputsHash("h", &movedFolder))
}

func TestPortForms(t *testing.T) {
	pf, err := ParsePortSpec("localhost:3000")
	require.NoError(t, err)
	assert.Equal(t, PortForward{HostPort: 3000, ContainerPort: 3000}, pf)

	// M12: 3000 and "localhost:3000" are one port.
	var image, local PortList
	require.NoError(t, image.UnmarshalJSON([]byte(`[3000]`)))
	require.NoError(t, local.UnmarshalJSON([]byte(`["localhost:3000", 4000]`)))
	assert.Len(t, image.union(local), 2)

	// R12: appPort numbers bind loopback, strings are published as written.
	var app PortList
	require.NoError(t, app.UnmarshalJSON([]byte(`[3000, "8080:80"]`)))
	b := NewBuilder(nil)
	got := b.parsePorts("appPort", app, true)
	assert.Equal(t, []PortForward{{HostPort: 3000, ContainerPort: 3000, Host: "127.0.0.1"}, {HostPort: 8080, ContainerPort: 80}}, got)
}

func TestPortCollisions(t *testing.T) {
	loop := PortForward{HostPort: 3000, ContainerPort: 3000, Host: "127.0.0.1"}
	assert.True(t, loop.Collides(PortForward{HostPort: 3000, ContainerPort: 80}), "every interface includes loopback")
	assert.True(t, loop.Collides(PortForward{HostPort: 3000, ContainerPort: 3000, Host: "localhost"}))
	assert.False(t, loop.Collides(PortForward{HostPort: 3000, ContainerPort: 3000, Host: "127.0.0.1", Protocol: "udp"}))
	assert.False(t, loop.Collides(PortForward{HostPort: 3000, ContainerPort: 3000, Host: "10.0.0.1"}))
	assert.False(t, loop.Collides(PortForward{ContainerPort: 3000, EphemeralHostPort: true}))
	assert.True(t, loop.SamePort(PortForward{ContainerPort: 3000, Protocol: "tcp"}))
}
