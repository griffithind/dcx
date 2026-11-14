// Package conformance checks dcx against the Dev Container specification
// (https://containers.dev/implementors/spec/, "Merge logic" and the
// property reference). Every case runs ResolveWorkspace, the pipeline
// `dcx up` uses, over a workspace on disk with the base image's
// devcontainer.metadata supplied by a fake image source; the unit tests in
// internal/devcontainer cover the individual parsers.
package conformance

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// images serves devcontainer.metadata labels by image reference.
type images map[string]string

func (i images) ImageMetadata(_ context.Context, ref string) (string, error) { return i[ref], nil }

// resolve writes files (path -> content, relative to a new workspace named
// "project") and resolves the workspace with the given image metadata.
func resolve(t *testing.T, files map[string]string, meta images) *devcontainer.ResolvedDevContainer {
	t.Helper()
	ws := filepath.Join(t.TempDir(), "project")
	for rel, content := range files {
		p := filepath.Join(ws, rel)
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}
	resolved, err := devcontainer.ResolveWorkspace(context.Background(), devcontainer.ResolveOptions{
		WorkspacePath: ws,
		Images:        meta,
		Lockfile:      devcontainer.LockfileIgnore,
	})
	require.NoError(t, err)
	return resolved
}

func config(json string) map[string]string {
	return map[string]string{".devcontainer/devcontainer.json": json}
}

// commands flattens a phase's hooks into "source: argv-or-shell" strings.
func commands(hooks []devcontainer.Hook) []string {
	var out []string
	for _, h := range hooks {
		for _, s := range h.Command.Steps() {
			cmd := s.Shell
			if cmd == "" {
				cmd = strings.Join(s.Argv, " ")
			}
			out = append(out, string(h.Source)+": "+cmd)
		}
	}
	return out
}

// Merge logic: image metadata first, devcontainer.json last.
func TestMergeLogic(t *testing.T) {
	meta := images{"base:1": `[
		{"remoteUser": "imageuser", "containerUser": "imagecontainer",
		 "init": true, "privileged": false,
		 "capAdd": ["SYS_PTRACE"], "securityOpt": ["seccomp=unconfined"],
		 "containerEnv": {"A": "image", "B": "image"},
		 "remoteEnv": {"R": "image"},
		 "mounts": ["type=volume,source=image-vol,target=/data", "type=volume,source=cache,target=/cache"],
		 "forwardPorts": [3000],
		 "onCreateCommand": "echo image-create",
		 "postStartCommand": "echo image-start",
		 "hostRequirements": {"cpus": 4, "memory": "4gb"}},
		{"capAdd": ["NET_ADMIN"], "entrypoint": "/usr/local/bin/image-entry"}
	]`}
	r := resolve(t, config(`{
		"image": "base:1",
		"remoteUser": "localuser",
		"privileged": true,
		"capAdd": ["SYS_PTRACE", "SYS_ADMIN"],
		"containerEnv": {"B": "local", "C": "local"},
		"mounts": ["type=volume,source=local-vol,target=/data"],
		"forwardPorts": [3000, 8080],
		"onCreateCommand": "echo local-create",
		"hostRequirements": {"cpus": 2, "memory": "8gb"}
	}`), meta)

	t.Run("single values: last wins", func(t *testing.T) {
		assert.Equal(t, "localuser", r.RemoteUser, "devcontainer.json overrides the image's remoteUser")
		assert.Equal(t, "imagecontainer", r.ContainerUser, "the image's value applies when the config has none")
	})
	t.Run("booleans: true if any is true", func(t *testing.T) {
		assert.True(t, r.Init, "init from the image")
		assert.True(t, r.Privileged, "privileged from the config")
	})
	t.Run("capAdd and securityOpt: union without duplicates", func(t *testing.T) {
		assert.ElementsMatch(t, []string{"SYS_PTRACE", "NET_ADMIN", "SYS_ADMIN"}, r.CapAdd)
		assert.Equal(t, []string{"seccomp=unconfined"}, r.SecurityOpt)
	})
	t.Run("env: per variable, last wins", func(t *testing.T) {
		assert.Equal(t, "image", r.ContainerEnv["A"])
		assert.Equal(t, "local", r.ContainerEnv["B"])
		assert.Equal(t, "local", r.ContainerEnv["C"])
		assert.Equal(t, "image", r.RemoteEnv["R"])
	})
	t.Run("mounts: collected, last source wins per target", func(t *testing.T) {
		byTarget := map[string]string{}
		for _, m := range r.Mounts {
			byTarget[m.Target] = m.Source
		}
		assert.Equal(t, "local-vol", byTarget["/data"])
		assert.Equal(t, "cache", byTarget["/cache"])
	})
	t.Run("forwardPorts: union", func(t *testing.T) {
		var ports []int
		for _, p := range r.ForwardPorts {
			ports = append(ports, p.ContainerPort)
		}
		assert.ElementsMatch(t, []int{3000, 8080}, ports)
	})
	t.Run("lifecycle commands: collected, image first", func(t *testing.T) {
		assert.Equal(t, []string{"image: echo image-create", "devcontainer.json: echo local-create"}, commands(r.Hooks.OnCreate))
		assert.Equal(t, []string{"image: echo image-start"}, commands(r.Hooks.PostStart))
	})
	t.Run("entrypoint: collected", func(t *testing.T) {
		assert.Equal(t, []string{"/usr/local/bin/image-entry"}, r.Entrypoints)
	})
	t.Run("hostRequirements: maximum of each", func(t *testing.T) {
		require.NotNil(t, r.HostRequirements)
		assert.Equal(t, 4, r.HostRequirements.CPUs)
		assert.Equal(t, "8gb", r.HostRequirements.Memory)
	})
}

// Image and Dockerfile plans: workspaceFolder defaults to
// /workspaces/<folder>.
func TestWorkspaceDefaults(t *testing.T) {
	r := resolve(t, config(`{"image": "base:1"}`), nil)
	assert.Equal(t, "/workspaces/project", r.WorkspaceFolder)
	assert.True(t, r.OverrideCommand, "overrideCommand defaults to true for image and Dockerfile plans")

	r = resolve(t, config(`{"image": "base:1", "workspaceFolder": "/src", "overrideCommand": false}`), nil)
	assert.Equal(t, "/src", r.WorkspaceFolder)
	assert.False(t, r.OverrideCommand)
}

// Compose plans: overrideCommand defaults to false, and the service and
// files come from the config.
func TestComposeDefaults(t *testing.T) {
	r := resolve(t, map[string]string{
		".devcontainer/devcontainer.json":  `{"dockerComposeFile": ["docker-compose.yml"], "service": "app", "workspaceFolder": "/workspace"}`,
		".devcontainer/docker-compose.yml": "services:\n  app:\n    image: base:1\n",
	}, images{"base:1": `[{"remoteUser": "composeuser"}]`})
	plan, ok := r.Plan.(*devcontainer.ComposePlan)
	require.True(t, ok, "compose plan, got %T", r.Plan)
	assert.Equal(t, "app", plan.Service)
	assert.False(t, r.OverrideCommand)
	assert.Equal(t, "composeuser", r.RemoteUser, "the service image's metadata applies")
}

// Variables: substituted in devcontainer.json and in image metadata alike;
// ${containerEnv:…} is left for exec time.
func TestVariableSubstitution(t *testing.T) {
	t.Setenv("DCX_CONFORMANCE_VAR", "from-host")
	r := resolve(t, config(`{
		"image": "base:1",
		"containerEnv": {
			"BASENAME": "${localWorkspaceFolderBasename}",
			"FOLDER": "${containerWorkspaceFolder}",
			"HOST": "${localEnv:DCX_CONFORMANCE_VAR}",
			"DEFAULTED": "${localEnv:DCX_CONFORMANCE_UNSET:fallback}",
			"ID": "${devcontainerId}"
		},
		"remoteEnv": {"P": "${containerEnv:PATH}:/extra"}
	}`), images{"base:1": `[{"mounts": ["type=volume,source=vol-${devcontainerId},target=/v"]}]`})

	assert.Equal(t, "project", r.ContainerEnv["BASENAME"])
	assert.Equal(t, "/workspaces/project", r.ContainerEnv["FOLDER"])
	assert.Equal(t, "from-host", r.ContainerEnv["HOST"])
	assert.Equal(t, "fallback", r.ContainerEnv["DEFAULTED"])
	id := r.ContainerEnv["ID"]
	assert.Equal(t, r.WorkspaceID, id, "${devcontainerId} is the workspace ID")
	assert.Equal(t, "${containerEnv:PATH}:/extra", r.RemoteEnv["P"])

	var sources []string
	for _, m := range r.Mounts {
		sources = append(sources, m.Source)
	}
	assert.Contains(t, sources, "vol-"+id, "image metadata is substituted too")
}

// Dockerfile plans read the metadata of the image the target stage is
// built FROM.
func TestDockerfileBaseMetadata(t *testing.T) {
	r := resolve(t, map[string]string{
		".devcontainer/devcontainer.json": `{"build": {"dockerfile": "Dockerfile", "target": "dev"}}`,
		".devcontainer/Dockerfile":        "FROM base:1 AS dev\nRUN true\nFROM other:2\n",
	}, images{"base:1": `[{"remoteUser": "devuser"}]`, "other:2": `[{"remoteUser": "wrong"}]`})
	_, ok := r.Plan.(*devcontainer.DockerfilePlan)
	require.True(t, ok)
	assert.Equal(t, "devuser", r.RemoteUser)
}
