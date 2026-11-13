package exec

import (
	"context"
	"sort"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/state"
)

func TestExecEnvironmentResolvesContainerEnvRefs(t *testing.T) {
	fake := &containertest.Fake{
		Containers: []state.ContainerSummary{{ID: "c1", Name: "c1"}},
		Env:        map[string][]string{"c1": {"PATH=/usr/local/bin:/usr/bin", "HOME=/root"}},
	}
	opts := ContainerExecOptions{
		ContainerName: "c1",
		Config: &devcontainer.DevContainerConfig{RemoteEnv: map[string]string{
			"PATH":  "${containerEnv:PATH}:/custom/bin",
			"OTHER": "plain",
		}},
		Ports: fake,
	}

	env := buildExecEnvironment("dev", opts.Config, containerEnvFor(context.Background(), opts))
	sort.Strings(env)
	want := []string{"OTHER=plain", "PATH=/usr/local/bin:/usr/bin:/custom/bin", "USER=dev"}
	if len(env) != len(want) {
		t.Fatalf("env = %v", env)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Errorf("env[%d] = %q, want %q", i, env[i], want[i])
		}
	}
}

func TestContainerEnvOnlyInspectedWhenReferenced(t *testing.T) {
	fake := &containertest.Fake{Containers: []state.ContainerSummary{{ID: "c1", Name: "c1"}}}
	opts := ContainerExecOptions{
		ContainerName: "c1",
		Config:        &devcontainer.DevContainerConfig{RemoteEnv: map[string]string{"A": "b"}},
		Ports:         fake,
	}
	if env := containerEnvFor(context.Background(), opts); env != nil {
		t.Errorf("expected nil, got %v", env)
	}
	if n := len(fake.CallsTo("InspectContainer")); n != 0 {
		t.Errorf("InspectContainer called %d times", n)
	}
}
