package container_test

import (
	"context"
	"os"
	"strconv"
	"testing"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/devcontainer"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const sshTestWorkspace = "ws_ssh_endpoint"

func singleRuntime(t *testing.T, fake *containertest.Fake, network string, adjust ...func(*devcontainer.ResolvedDevContainer)) *container.Provisioner {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	resolved := &devcontainer.ResolvedDevContainer{
		Identity:  devcontainer.Identity{WorkspaceID: sshTestWorkspace, ResourceName: "app-ws"},
		BaseImage: "alpine:3",
		Plan:      &devcontainer.ImagePlan{Image: "alpine:3"},
		Config:    &devcontainer.DevContainerConfig{},
	}
	if network != "" {
		resolved.RunArgs = &devcontainer.ParsedRunArgs{NetworkMode: network}
	}
	for _, f := range adjust {
		f(resolved)
	}
	fake.Registry = map[string]map[string]string{"alpine:3": {}}
	prov, err := container.NewProvisioner(fake, nil, resolved)
	require.NoError(t, err)
	return prov
}

func sshPorts(spec container.ContainerSpec) []devcontainer.PortForward {
	var out []devcontainer.PortForward
	for _, p := range spec.Ports {
		if p.ContainerPort == agentproto.ListenPort {
			out = append(out, p)
		}
	}
	return out
}

func TestSSHPortIsPublishedAndRecorded(t *testing.T) {
	fake := &containertest.Fake{}
	require.NoError(t, up(t, singleRuntime(t, fake, ""), container.UpOptions{}))
	require.Len(t, fake.Specs, 1)
	spec := fake.Specs[0]

	published := sshPorts(spec)
	require.Len(t, published, 1)
	assert.Equal(t, "127.0.0.1", published[0].Host)
	labels := state.ContainerLabelsFromMap(spec.Labels)
	assert.Equal(t, published[0].HostPort, labels.SSHPort, "the published port is recorded")
	assert.Equal(t, agentproto.ListenAddr("0.0.0.0"), labels.SSHListen)
	assert.Empty(t, labels.SSHAllowedCIDRs)
}

func TestSSHPortSkipsPortsOfOtherWorkspaces(t *testing.T) {
	// A stopped container of another workspace holds the preferred port:
	// nothing is bound now, but starting it later would collide.
	preferred := dcxssh.DeterministicPort(sshTestWorkspace)
	other := state.NewContainerLabels()
	other.WorkspaceID = "ws_other"
	other.SSHPort = preferred
	fake := &containertest.Fake{Containers: []state.ContainerSummary{{ID: "c-other", Labels: other.ToMap()}}}

	require.NoError(t, up(t, singleRuntime(t, fake, ""), container.UpOptions{}))
	labels := state.ContainerLabelsFromMap(fake.Specs[0].Labels)
	assert.NotEqual(t, preferred, labels.SSHPort)
	assert.Equal(t, labels.SSHPort, sshPorts(fake.Specs[0])[0].HostPort)
}

func TestWidenedSSHAccessIsPublishedOnAllInterfacesAndRecorded(t *testing.T) {
	fake := &containertest.Fake{}
	access := container.SSHAccess{AllowedCIDRs: []string{"10.0.0.0/24"}}
	require.NoError(t, up(t, singleRuntime(t, fake, ""), container.UpOptions{}, access))
	spec := fake.Specs[0]
	assert.Equal(t, "0.0.0.0", sshPorts(spec)[0].Host)
	assert.Equal(t, []string{"10.0.0.0/24"}, state.ContainerLabelsFromMap(spec.Labels).SSHAllowedCIDRs)
}

func TestHostNetworkAgentListensOnTheHostPort(t *testing.T) {
	fake := &containertest.Fake{}
	require.NoError(t, up(t, singleRuntime(t, fake, "host"), container.UpOptions{}))
	spec := fake.Specs[0]
	assert.Empty(t, sshPorts(spec), "Docker can't publish ports with host networking")
	labels := state.ContainerLabelsFromMap(spec.Labels)
	require.NotZero(t, labels.SSHPort)
	assert.Equal(t, "127.0.0.1:"+strconv.Itoa(labels.SSHPort), labels.SSHListen)
}

func TestContainerNetworkModeIsRejected(t *testing.T) {
	fake := &containertest.Fake{}
	prov := singleRuntime(t, fake, "container:db")
	err := prov.Build(context.Background(), container.UpOptions{})
	require.Error(t, err, "rejected before a recreate removes anything")
	assert.Contains(t, err.Error(), "container:db")
	assert.Empty(t, fake.Specs)
}

func TestComposeSharedNetworkPublishesOnTheOwner(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := &containertest.Fake{
		ComposeOutputs: map[string]string{"--profile * config --format json": `{"services":{
			"app":{"image":"mcr.example/base:1","network_mode":"service:db"},
			"db":{"image":"postgres:16"}}}`},
		Registry: map[string]map[string]string{"mcr.example/base:1": {}},
	}
	prov, err := container.NewProvisioner(fake, nil, &devcontainer.ResolvedDevContainer{
		Identity:     devcontainer.Identity{WorkspaceID: sshTestWorkspace, ResourceName: "app-ws"},
		Plan:         devcontainer.NewComposePlan([]string{"/w/compose.yml"}, "app", "proj"),
		Config:       &devcontainer.DevContainerConfig{},
		ForwardPorts: []devcontainer.PortForward{{ContainerPort: 5432, HostPort: 5432}},
	})
	require.NoError(t, err)
	require.NoError(t, up(t, prov, container.UpOptions{}))

	path, err := container.ComposeOverridePath(sshTestWorkspace)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var override struct {
		Services map[string]struct {
			Ports  []string          `yaml:"ports"`
			Labels map[string]string `yaml:"labels"`
		} `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(data, &override))

	port := state.ContainerLabelsFromMap(override.Services["app"].Labels).SSHPort
	require.NotZero(t, port)
	assert.Empty(t, override.Services["app"].Ports, "a service sharing another's network can't publish ports")
	assert.Equal(t, []string{"127.0.0.1:5432:5432", "127.0.0.1:" + strconv.Itoa(port) + ":" + strconv.Itoa(agentproto.ListenPort)},
		override.Services["db"].Ports, "user ports go to the network owner too")
}

func TestSSHHostPortPrefersTheRecordedPort(t *testing.T) {
	labels := state.NewContainerLabels()
	labels.SSHPort = 41234
	port, err := container.SSHHostPort(context.Background(), &containertest.Fake{}, &state.ContainerInfo{Name: "c", Labels: labels})
	require.NoError(t, err)
	assert.Equal(t, 41234, port)
}

// Ports without a host address bind loopback (F2): forwardPorts used to be
// published on every interface, and appPort's literal "localhost" was
// rejected by Docker ("invalid IP address: localhost").
func TestUserPortsBindLoopback(t *testing.T) {
	fake := &containertest.Fake{}
	prov := singleRuntime(t, fake, "", func(r *devcontainer.ResolvedDevContainer) {
		r.ForwardPorts = []devcontainer.PortForward{{ContainerPort: 3000, HostPort: 3000}, {ContainerPort: 80, HostPort: 8080}}
		r.AppPorts = []devcontainer.PortForward{{ContainerPort: 9000, HostPort: 9000, Host: "0.0.0.0"}}
	})
	require.NoError(t, up(t, prov, container.UpOptions{}))
	var specs []string
	for _, p := range fake.Specs[0].Ports {
		if p.ContainerPort != agentproto.ListenPort {
			specs = append(specs, p.DockerSpec())
		}
	}
	assert.Equal(t, []string{"0.0.0.0:9000:9000", "127.0.0.1:3000:3000", "127.0.0.1:8080:80"}, specs)
}

func TestHostNetworkPublishesNoUserPorts(t *testing.T) {
	fake := &containertest.Fake{}
	prov := singleRuntime(t, fake, "host", func(r *devcontainer.ResolvedDevContainer) {
		r.ForwardPorts = []devcontainer.PortForward{{ContainerPort: 3000, HostPort: 3000}}
	})
	require.NoError(t, up(t, prov, container.UpOptions{}))
	assert.Empty(t, fake.Specs[0].Ports, "Docker discards published ports with host networking")
}
