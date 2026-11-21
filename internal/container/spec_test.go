package container

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/build"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// fullSpec sets every ContainerSpec field.
func fullSpec() ContainerSpec {
	return ContainerSpec{
		Name:        "ws-1234abcd",
		Image:       "img:1",
		User:        "vscode",
		Labels:      map[string]string{"l": "v"},
		Env:         map[string]string{"E": "v"},
		Mounts:      []devcontainer.Mount{{Type: "bind", Source: "/a", Target: "/b"}},
		Tmpfs:       map[string]string{"/t": ""},
		Ports:       []devcontainer.PortForward{{ContainerPort: 80, HostPort: 8080}},
		CapAdd:      []string{"SYS_PTRACE"},
		CapDrop:     []string{"NET_RAW"},
		SecurityOpt: []string{"seccomp=unconfined"},
		Privileged:  true,
		Init:        true,
		NetworkMode: "host",
		IpcMode:     "host",
		PidMode:     "host",
		ShmSize:     1024,
		Devices:     []string{"/dev/fuse"},
		ExtraHosts:  []string{"h:1.2.3.4"},
		Sysctls:     map[string]string{"net.core.somaxconn": "1024"},
		GPUs:        "all",
		Entrypoint:  []string{"/bin/sh", "-c", "x"},
		Cmd:         []string{"cmd-marker"},
		RunArgs:     []string{"--runarg-marker"},
	}
}

// TestContainerSpecParity asserts every ContainerSpec field is rendered by
// both engines, except the documented docker-only ones. Adding a field
// without rendering it (or listing it here) fails the test, which is what
// keeps compose from silently dropping settings again.
func TestContainerSpecParity(t *testing.T) {
	type rendering struct {
		docker  string // a docker arg that must appear
		compose string // a service key that must appear ("" = docker-only)
	}
	table := map[string]rendering{
		"Name":        {"--name", ""},
		"Image":       {"img:1", "image"},
		"User":        {"-u", "user"},
		"Labels":      {"-l", "labels"},
		"Env":         {"-e", "environment"},
		"Mounts":      {"--mount", "volumes"},
		"Tmpfs":       {"--tmpfs", "tmpfs"},
		"Ports":       {"-p", "ports"},
		"CapAdd":      {"--cap-add", "cap_add"},
		"CapDrop":     {"--cap-drop", "cap_drop"},
		"SecurityOpt": {"--security-opt", "security_opt"},
		"Privileged":  {"--privileged", "privileged"},
		"Init":        {"--init", "init"},
		"NetworkMode": {"--network", "network_mode"},
		"IpcMode":     {"--ipc", "ipc"},
		"PidMode":     {"--pid", "pid"},
		"ShmSize":     {"--shm-size", "shm_size"},
		"Devices":     {"--device", "devices"},
		"ExtraHosts":  {"--add-host", "extra_hosts"},
		"Sysctls":     {"--sysctl", "sysctls"},
		"GPUs":        {"--gpus", "deploy"},
		"Entrypoint":  {"--entrypoint", "entrypoint"},
		"Cmd":         {"cmd-marker", "command"},
		"RunArgs":     {"--runarg-marker", ""},
	}

	fields := reflect.VisibleFields(reflect.TypeOf(ContainerSpec{}))
	for _, f := range fields {
		_, ok := table[f.Name]
		assert.Truef(t, ok, "ContainerSpec.%s is not covered by the parity table; render it in dockerRunArgs and composeOverride and add it here", f.Name)
	}
	assert.Len(t, table, len(fields), "parity table lists a field ContainerSpec no longer has")

	// Every field is rendered when set on its own, so one field's output
	// can't mask another's absence.
	full := reflect.ValueOf(fullSpec())
	for _, f := range fields {
		want := table[f.Name]
		t.Run(f.Name, func(t *testing.T) {
			var s ContainerSpec
			reflect.ValueOf(&s).Elem().FieldByIndex(f.Index).Set(full.FieldByIndex(f.Index))
			if f.Name != "Image" {
				s.Image = "base"
			}

			assert.Contains(t, dockerRunArgs(s), want.docker)

			svc := renderComposeService(t, s)
			if want.compose == "" {
				return
			}
			assert.Containsf(t, svc, want.compose, "compose override for %s: %v", f.Name, svc)
		})
	}
}

func renderComposeService(t *testing.T, s ContainerSpec) map[string]any {
	t.Helper()
	out, err := composeOverride("app", s, nil)
	require.NoError(t, err)
	var doc struct {
		Services map[string]map[string]any `yaml:"services"`
	}
	require.NoError(t, yaml.Unmarshal(out, &doc), string(out))
	return doc.Services["app"]
}

func TestComposeOverrideEscapesDollar(t *testing.T) {
	s := ContainerSpec{
		Env:    map[string]string{"PS1": "$USER> ", "PATH": "/x:${PATH}"},
		Labels: map[string]string{"l": "a$b"},
		Mounts: []devcontainer.Mount{{Type: "bind", Source: "/src/$dir", Target: "/t"}},
	}
	out, err := composeOverride("app", s, nil)
	require.NoError(t, err)
	text := string(out)
	assert.Contains(t, text, "$$USER> ")
	assert.Contains(t, text, "/x:$${PATH}")
	assert.Contains(t, text, "a$$b")
	assert.Contains(t, text, "/src/$$dir")
}

func TestComposeOverrideDeclaresNamedVolumes(t *testing.T) {
	s := ContainerSpec{Mounts: []devcontainer.Mount{
		{Type: "volume", Source: "cache-vol", Target: "/cache"},
		{Type: "bind", Source: "/host", Target: "/h", ReadOnly: true},
	}}
	out, err := composeOverride("app", s, nil)
	require.NoError(t, err)

	var doc struct {
		Services map[string]struct {
			Volumes []composeVolume `yaml:"volumes"`
		} `yaml:"services"`
		Volumes map[string]composeTopVolume `yaml:"volumes"`
	}
	require.NoError(t, yaml.Unmarshal(out, &doc))
	assert.Equal(t, []composeVolume{
		{Type: "volume", Source: "cache-vol", Target: "/cache"},
		{Type: "bind", Source: "/host", Target: "/h", ReadOnly: true},
	}, doc.Services["app"].Volumes)
	// Declared with an explicit name, so compose uses "cache-vol" rather
	// than "<project>_cache-vol" — the same volume docker run would use.
	assert.Equal(t, map[string]composeTopVolume{"cache-vol": {Name: "cache-vol"}}, doc.Volumes)
}

func TestComposeOverrideGPUAndSecrets(t *testing.T) {
	out, err := composeOverride("app", ContainerSpec{GPUs: "2"}, map[string]string{"tok": "/tmp/tok"})
	require.NoError(t, err)
	text := string(out)
	assert.Contains(t, text, "count: 2\n")
	assert.Contains(t, text, "driver: nvidia")
	assert.Contains(t, text, "build:\n            secrets:\n                - tok")
	assert.Contains(t, text, "tok:\n        file: /tmp/tok")
}

func TestComposeOverrideIsDeterministic(t *testing.T) {
	s := fullSpec()
	s.Env = map[string]string{"A": "1", "B": "2", "C": "3"}
	s.Tmpfs = map[string]string{"/a": "", "/b": "size=1m"}
	first, err := composeOverride("app", s, nil)
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		again, err := composeOverride("app", s, nil)
		require.NoError(t, err)
		assert.Equal(t, string(first), string(again))
		assert.Equal(t, dockerRunArgs(s), dockerRunArgs(s))
	}
}

func TestDockerRunArgsEntrypoint(t *testing.T) {
	args := dockerRunArgs(ContainerSpec{Image: "img", Entrypoint: keepAliveEntrypoint, Cmd: []string{"cmd"}})
	i := slices.Index(args, "--entrypoint")
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "/bin/sh", args[i+1])
	// Image, then the rest of the entrypoint as the command.
	assert.Equal(t, append(append([]string{"img"}, keepAliveEntrypoint[1:]...), "cmd"), args[len(args)-5:])

	args = dockerRunArgs(ContainerSpec{Image: "img", Cmd: []string{"a", "b"}})
	assert.Equal(t, []string{"img", "a", "b"}, args[len(args)-3:])
	assert.Equal(t, "create", args[0])
	assert.NotContains(t, args, "-t", "no TTY, as devcontainers/cli creates it (R13)")
	assert.NotContains(t, args, "-i")
}

func gpuRuntime(plan devcontainer.ExecutionPlan) *Provisioner {
	return &Provisioner{report: report.Discard, resolved: &devcontainer.ResolvedDevContainer{
		Identity:        devcontainer.Identity{WorkspaceID: "1234abcd5678", Name: "ws", ResourceName: "ws-1234abcd"},
		LocalRoot:       "/home/u/ws",
		WorkspaceFolder: "/workspaces/ws",
		WorkspaceMount:  &devcontainer.Mount{Type: "bind", Source: "/home/u/ws", Target: "/workspaces/ws"},
		Plan:            plan,
		ContainerUser:   "vscode",
		CapAdd:          []string{"SYS_ADMIN", "NET_ADMIN"}, // the builder merges feature caps
		SecurityOpt:     []string{"seccomp=unconfined"},
		Privileged:      true,
		Init:            true,
		AppPorts:        []devcontainer.PortForward{{ContainerPort: 3000, HostPort: 3000}},
		GPU:             &devcontainer.GPURequirement{Mode: devcontainer.GPURequired},
		RunArgs:         &devcontainer.ParsedRunArgs{IpcMode: "host", Passthrough: []string{"--hostname=x"}},
		Features: []*features.Feature{{ID: "dind", Metadata: &features.FeatureMetadata{
			ID: "dind", CapAdd: []string{"NET_ADMIN"}, SecurityOpt: []string{"seccomp=unconfined"},
		}}},
		Config:          &devcontainer.DevContainerConfig{},
		OverrideCommand: plan.Type() != devcontainer.PlanTypeCompose,
	}}
}

// TestSpecComposeParity is the W8 regression: settings that used to be
// dropped for compose (caps, privileged, init, user, feature security,
// GPU, appPorts) are in the compose spec too.
func TestSpecComposeParity(t *testing.T) {
	single := gpuRuntime(devcontainer.NewImagePlan("img")).spec("img")
	compose := gpuRuntime(devcontainer.NewComposePlan([]string{"/x/compose.yml"}, "app", "proj")).spec("")

	for name, s := range map[string]ContainerSpec{"single": single, "compose": compose} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, "vscode", s.User)
			assert.ElementsMatch(t, []string{"SYS_ADMIN", "NET_ADMIN"}, s.CapAdd)
			assert.Equal(t, []string{"seccomp=unconfined"}, s.SecurityOpt)
			assert.True(t, s.Privileged)
			assert.True(t, s.Init)
			assert.Equal(t, "all", s.GPUs)
			assert.Equal(t, "ws", s.Env["DCX_PROJECT_NAME"])
			assert.Equal(t, "1234abcd5678", s.Labels[state.LabelWorkspaceID])
			assert.Contains(t, s.Tmpfs, "/run/secrets")
			var containerPorts []int
			for _, p := range s.Ports {
				containerPorts = append(containerPorts, p.ContainerPort)
			}
			assert.Contains(t, containerPorts, 3000)
		})
	}

	// Single-container only: name, workspace mount, runArgs and
	// the keep-alive (overrideCommand defaults to false for compose).
	assert.Equal(t, "ws-1234abcd", single.Name)
	assert.Equal(t, devcontainer.Mount{Type: "bind", Source: "/home/u/ws", Target: "/workspaces/ws"}, single.Mounts[0])
	assert.Equal(t, "host", single.IpcMode)
	assert.Equal(t, []string{"--hostname=x"}, single.RunArgs)
	assert.Equal(t, keepAliveEntrypoint, single.Entrypoint)

	assert.Empty(t, compose.Name)
	assert.Empty(t, compose.Mounts)
	assert.Empty(t, compose.IpcMode)
	assert.Empty(t, compose.RunArgs)
	assert.Empty(t, compose.Entrypoint)
}

func TestComposeServices(t *testing.T) {
	assert.Nil(t, composeServices(&devcontainer.ComposePlan{Service: "app"}))
	assert.Equal(t, []string{"db", "app"}, composeServices(&devcontainer.ComposePlan{Service: "app", RunServices: []string{"db"}}))
	assert.Equal(t, []string{"app", "db"}, composeServices(&devcontainer.ComposePlan{Service: "app", RunServices: []string{"app", "db"}}))
}

func TestExistingComposeProject(t *testing.T) {
	dir := t.TempDir()
	compose := filepath.Join(dir, "compose.yml")
	require.NoError(t, os.WriteFile(compose, []byte("services: {}\n"), 0o644))

	info := &state.ContainerInfo{
		ComposeProject: "proj",
		Labels: &state.ContainerLabels{
			ComposeWorkingDir:  dir,
			ComposeConfigFiles: []string{compose, "/gone/dcx-override-123.yml", "relative.yml"},
		},
	}
	p := ExistingComposeProject(info, "default", "/fallback")
	assert.Equal(t, "proj", p.Name)
	assert.Equal(t, dir, p.Dir)
	assert.Equal(t, []string{compose}, p.Files, "missing files are skipped")
	assert.Equal(t, []string{"compose", "-p", "proj", "-f", compose, "down"}, p.args("down"))

	// No recorded file survives: run from an empty directory with -p only,
	// so compose cannot load an unrelated default-named file.
	info.Labels.ComposeConfigFiles = []string{"/gone/a.yml"}
	p = ExistingComposeProject(info, "default", "/fallback")
	assert.Empty(t, p.Files)
	assert.Equal(t, os.TempDir(), p.Dir)
	assert.True(t, strings.HasPrefix(strings.Join(p.args("stop"), " "), "compose -p proj stop"))
}

func TestBuildArgsAreDeterministic(t *testing.T) {
	opts := build.DockerfileBuildOptions{
		Tag:           "dcx/ws:base",
		Dockerfile:    "/w/Dockerfile",
		Context:       "/w",
		Args:          map[string]string{"B": "2", "A": "1"},
		Secrets:       map[string]string{"tok": "/tmp/tok"},
		BuildContexts: map[string]string{"features": "/tmp/f"},
		Options:       []string{"--network=host"},
	}
	assert.Equal(t, []string{
		"buildx", "build", "-t", "dcx/ws:base", "-f", "/w/Dockerfile",
		"--build-arg", "A=1", "--build-arg", "B=2",
		"--build-context", "features=/tmp/f",
		"--secret", "id=tok,src=/tmp/tok",
		"--network=host", "--load", "/w",
	}, buildArgs(opts, true))

	// Without buildx: plain `docker build` (BuildKit), no --load (R14).
	plain := buildArgs(build.DockerfileBuildOptions{Tag: "t", Context: "/w"}, false)
	assert.Equal(t, []string{"build", "-t", "t", "/w"}, plain)
}

// Mount rendering (F16, F20, Q4): CSV quoting, passed-through options,
// SELinux relabelling, tmpfs options.
func TestMountRendering(t *testing.T) {
	comma := devcontainer.Mount{Type: "bind", Source: "/home/u/a,b", Target: "/w"}
	assert.Equal(t, `type=bind,"source=/home/u/a,b",target=/w`, formatMount(&comma))

	prop := devcontainer.Mount{Type: "bind", Source: "/src", Target: "/dst", Options: []string{"bind-propagation=rshared"}}
	assert.Equal(t, "type=bind,source=/src,target=/dst,bind-propagation=rshared", formatMount(&prop))

	relabel := ContainerSpec{Image: "img", Mounts: []devcontainer.Mount{{Type: "bind", Source: "/ws", Target: "/workspaces/ws", Relabel: true}}}
	args := dockerRunArgs(relabel)
	i := slices.Index(args, "-v")
	require.GreaterOrEqual(t, i, 0)
	assert.Equal(t, "/ws:/workspaces/ws:z", args[i+1])

	out, err := composeOverride("app", ContainerSpec{Mounts: []devcontainer.Mount{
		{Type: "bind", Source: "/ws", Target: "/w", Relabel: true, Options: []string{"bind-propagation=rslave"}},
		{Type: "volume", Source: "data", Target: "/d", Options: []string{"volume-subpath=sub", "volume-nocopy=true"}},
	}}, nil)
	require.NoError(t, err)
	assert.Contains(t, string(out), "propagation: rslave")
	assert.Contains(t, string(out), "selinux: z")
	assert.Contains(t, string(out), "subpath: sub")
	assert.Contains(t, string(out), "nocopy: true")

	assert.Equal(t, "size=64m,mode=1777", tmpfsOptions(devcontainer.Mount{Type: "tmpfs", Target: "/t", Options: []string{"tmpfs-size=64m", "tmpfs-mode=1777"}}))
}
