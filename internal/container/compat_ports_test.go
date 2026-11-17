package container_test

import (
	"context"
	"os"
	"testing"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func userPorts(spec container.ContainerSpec) []string {
	var out []string
	for _, p := range spec.Ports {
		if p.ContainerPort != agentproto.ListenPort {
			out = append(out, p.DockerSpec())
		}
	}
	return out
}

// R1/M12: a port listed twice (appPort and forwardPorts, or 3000 and
// "localhost:3000") is published once; another container port on a host
// port already taken is skipped instead of failing the create.
func TestPortsArePublishedOnce(t *testing.T) {
	fake := &containertest.Fake{}
	prov := singleRuntime(t, fake, "", func(r *devcontainer.ResolvedDevContainer) {
		r.AppPorts = []devcontainer.PortForward{{ContainerPort: 3000, HostPort: 3000, Host: "127.0.0.1"}}
		r.ForwardPorts = []devcontainer.PortForward{
			{ContainerPort: 3000, HostPort: 3000},
			{ContainerPort: 3000, HostPort: 3000, Host: "localhost"},
			{ContainerPort: 80, HostPort: 3000},
			{ContainerPort: 5432, HostPort: 5432},
		}
	})
	require.NoError(t, prov.Up(context.Background(), container.UpOptions{}))
	assert.Equal(t, []string{"127.0.0.1:3000:3000", "127.0.0.1:5432:5432"}, userPorts(fake.Specs[0]))
}

// R1/R8: a port the compose file already publishes is not published again,
// and a volume the compose file declares is not re-declared with a name.
func TestComposeOverrideLeavesTheUsersPortsAndVolumes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := &containertest.Fake{
		ComposeOutputs: map[string]string{"--profile * config --format json": `{
			"services":{"app":{"image":"base:1","ports":[{"target":3000,"published":"3000","protocol":"tcp"}]}},
			"volumes":{"pgdata":{"name":"proj_pgdata"}}}`},
		Registry: map[string]map[string]string{"base:1": {}},
	}
	resolved := &devcontainer.ResolvedDevContainer{
		Identity:     devcontainer.Identity{WorkspaceID: "ws1234567890", Name: "app", ResourceName: "app-ws123456"},
		Plan:         devcontainer.NewComposePlan([]string{"/w/compose.yml"}, "app", "proj"),
		Config:       &devcontainer.DevContainerConfig{},
		ForwardPorts: []devcontainer.PortForward{{ContainerPort: 3000, HostPort: 3000}, {ContainerPort: 5432, HostPort: 5432}},
		Mounts: []devcontainer.Mount{
			{Type: "volume", Source: "pgdata", Target: "/var/lib/postgresql/data"},
			{Type: "volume", Source: "cache", Target: "/cache"},
		},
	}
	prov, err := container.NewProvisioner(fake, nil, resolved)
	require.NoError(t, err)
	require.NoError(t, prov.Up(context.Background(), container.UpOptions{}))

	path, err := container.ComposeOverridePath("ws1234567890")
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var override struct {
		Services map[string]struct {
			Ports []string `yaml:"ports"`
		} `yaml:"services"`
		Volumes map[string]struct {
			Name string `yaml:"name"`
		} `yaml:"volumes"`
	}
	require.NoError(t, yaml.Unmarshal(data, &override))
	assert.Contains(t, override.Services["app"].Ports, "127.0.0.1:5432:5432")
	assert.NotContains(t, override.Services["app"].Ports, "127.0.0.1:3000:3000")
	assert.NotContains(t, override.Volumes, "pgdata")
	assert.Equal(t, "cache", override.Volumes["cache"].Name)
}

// M10: the container carries the merged metadata, including the local
// config, even when dcx builds no layer; R5: runArgs --name names it.
func TestContainerMetadataLabelAndName(t *testing.T) {
	fake := &containertest.Fake{}
	prov := singleRuntime(t, fake, "", func(r *devcontainer.ResolvedDevContainer) {
		r.Local = &devcontainer.DevContainerConfig{Image: "alpine:3", RemoteUser: "vscode"}
		r.RunArgs = &devcontainer.ParsedRunArgs{Name: "myproj_devcontainer"}
	})
	require.NoError(t, prov.Up(context.Background(), container.UpOptions{}))
	spec := fake.Specs[0]
	assert.Equal(t, "myproj_devcontainer", spec.Name)
	assert.Contains(t, spec.Labels[devcontainer.DevcontainerMetadataLabel], `"remoteUser":"vscode"`)
}
