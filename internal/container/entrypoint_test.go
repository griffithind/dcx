package container

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/internal/build"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cmdEngine answers the two engine calls prepareCommand makes.
type cmdEngine struct {
	Engine
	images  map[string][2][]string
	compose string
}

func (e *cmdEngine) ImageConfig(_ context.Context, ref string) (build.ImageConfig, error) {
	c := e.images[ref]
	return build.ImageConfig{Entrypoint: c[0], Cmd: c[1]}, nil
}

func (e *cmdEngine) ComposeOutput(context.Context, ComposeProject, ...string) ([]byte, error) {
	return []byte(e.compose), nil
}

func entrypointRuntime(fake *cmdEngine, plan devcontainer.ExecutionPlan, override bool) *Provisioner {
	return &Provisioner{engine: fake, report: report.Discard, resolved: &devcontainer.ResolvedDevContainer{
		Plan:            plan,
		OverrideCommand: override,
		Entrypoints:     []string{"/usr/local/share/docker-init.sh", "/other-init.sh"},
		Features: []*features.Feature{
			{ID: "dind", Metadata: &features.FeatureMetadata{ID: "dind", Entrypoint: "/usr/local/share/docker-init.sh"}},
			{ID: "plain", Metadata: &features.FeatureMetadata{ID: "plain"}},
			{ID: "other", Metadata: &features.FeatureMetadata{ID: "other", Entrypoint: "/other-init.sh"}},
		},
	}}
}

func TestFeatureEntrypointsRunBeforeTheKeepAlive(t *testing.T) {
	r := entrypointRuntime(&cmdEngine{}, devcontainer.NewImagePlan("img"), true)
	require.NoError(t, r.prepareCommand(context.Background(), "img"))
	s := r.spec("img")
	assert.Equal(t, []string{"dcx-keepalive", "2", "/usr/local/share/docker-init.sh", "/other-init.sh"}, s.Entrypoint[3:])
	assert.Empty(t, s.Cmd)
}

func TestFeatureEntrypointsChainTheImageCommand(t *testing.T) {
	fake := &cmdEngine{images: map[string][2][]string{
		"img": {{"/entry.sh"}, {"serve", "--port", "80"}},
	}}
	r := entrypointRuntime(fake, devcontainer.NewImagePlan("img"), false)
	require.NoError(t, r.prepareCommand(context.Background(), "img"))
	s := r.spec("img")
	assert.Equal(t, chainEntrypoints([]string{"/usr/local/share/docker-init.sh", "/other-init.sh"}), s.Entrypoint)
	assert.Equal(t, []string{"/entry.sh", "serve", "--port", "80"}, s.Cmd, "the image's ENTRYPOINT and CMD still run")
}

func TestFeatureEntrypointsChainTheComposeCommand(t *testing.T) {
	fake := &cmdEngine{
		images:  map[string][2][]string{"img": {{"/entry.sh"}, {"default"}}},
		compose: `{"services":{"app":{"image":"img","command":["npm","start"]}}}`,
	}
	r := entrypointRuntime(fake, devcontainer.NewComposePlan([]string{"/w/compose.yml"}, "app", "proj"), false)
	require.NoError(t, r.prepareCommand(context.Background(), "img"))
	assert.Equal(t, []string{"/entry.sh", "npm", "start"}, r.spec("img").Cmd, "the service command replaces the image CMD")

	fake.compose = `{"services":{"app":{"image":"img","entrypoint":"run.sh"}}}`
	r = entrypointRuntime(fake, devcontainer.NewComposePlan([]string{"/w/compose.yml"}, "app", "proj"), false)
	require.NoError(t, r.prepareCommand(context.Background(), "img"))
	assert.Equal(t, []string{"/bin/sh", "-c", "run.sh"}, r.spec("img").Cmd, "a service entrypoint drops the image CMD")
}

func TestNoFeatureEntrypointsLeavesTheCommandAlone(t *testing.T) {
	r := &Provisioner{engine: &cmdEngine{}, report: report.Discard, resolved: &devcontainer.ResolvedDevContainer{
		Plan: devcontainer.NewComposePlan([]string{"/w/compose.yml"}, "app", "proj"),
	}}
	require.NoError(t, r.prepareCommand(context.Background(), "img"))
	s := r.spec("img")
	assert.Empty(t, s.Entrypoint)
	assert.Empty(t, s.Cmd)
}

// The chain runs each entrypoint (without arguments) in order, then execs
// the command with its arguments intact.
func TestChainScriptRunsEntrypointsThenTheCommand(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := func(name string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte("#!/bin/sh\necho \""+name+" $#\" >> "+log+"\nexec \"$@\"\n"), 0o755))
		return p
	}
	argv := append(chainEntrypoints([]string{script("first"), script("second")}), "sh", "-c", `echo "cmd $1" >> "$0"`, log, "a b")
	require.NoError(t, exec.Command(argv[0], argv[1:]...).Run())
	out, err := os.ReadFile(log)
	require.NoError(t, err)
	assert.Equal(t, "first 0\nsecond 0\ncmd a b\n", string(out))
}
