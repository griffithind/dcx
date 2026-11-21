package devcontainer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These cover configs the reference implementation (devcontainers/cli)
// accepts and dcx used to reject or read differently (docs/compatibility.md).

func TestParseScalarValuesInStringMaps(t *testing.T) {
	cfg, err := Parse([]byte(`{
		"image": "alpine",
		"containerEnv": {"PORT": 3000, "DEBUG": true, "NAME": "x"},
		"remoteEnv": {"RATIO": 1.5, "GONE": null},
		"build": {"dockerfile": "Dockerfile", "args": {"NODE_VERSION": 20}}
	}`))
	require.NoError(t, err)
	assert.Equal(t, StringMap{"PORT": "3000", "DEBUG": "true", "NAME": "x"}, cfg.ContainerEnv)
	require.NotNil(t, cfg.RemoteEnv["RATIO"])
	assert.Equal(t, "1.5", *cfg.RemoteEnv["RATIO"])
	assert.Nil(t, cfg.RemoteEnv["GONE"], "null still unsets")
	assert.Equal(t, "20", cfg.Build.Args["NODE_VERSION"])

	_, err = Parse([]byte(`{"image": "alpine", "containerEnv": {"X": {"nested": 1}}}`))
	require.Error(t, err, "objects are not scalar values")
}

func TestParseCacheFromStringOrArray(t *testing.T) {
	for _, tc := range []struct{ in, out string }{
		{`"ghcr.io/org/app:cache"`, `"ghcr.io/org/app:cache"`},
		{`["a"]`, `["a"]`}, // written back as read: the config hash is unchanged
		{`["a", "b"]`, `["a","b"]`},
	} {
		cfg, err := Parse([]byte(`{"build": {"dockerfile": "Dockerfile", "cacheFrom": ` + tc.in + `}}`))
		require.NoError(t, err, tc.in)
		out, err := json.Marshal(cfg.Build.CacheFrom)
		require.NoError(t, err)
		assert.Equal(t, tc.out, string(out))
	}
	cfg, err := Parse([]byte(`{"build": {"dockerfile": "Dockerfile", "cacheFrom": "x"}}`))
	require.NoError(t, err)
	assert.Equal(t, []string{"x"}, cfg.Build.CacheFrom.List())
}

func TestParseLegacyDockerFileWithBuildObject(t *testing.T) {
	cfg, err := Parse([]byte(`{"dockerFile": "Dockerfile.dev", "context": "..", "build": {"dockerfile": "ignored", "context": "ignored", "args": {"VARIANT": "3.11"}}}`))
	require.NoError(t, err)
	assert.Equal(t, "Dockerfile.dev", cfg.Build.Dockerfile, "the top-level dockerFile wins")
	assert.Equal(t, "..", cfg.Build.Context, "and the context is the top-level one")
	assert.Equal(t, "3.11", cfg.Build.Args["VARIANT"], "build.args still apply")
	assert.Empty(t, cfg.LegacyDockerFile)
}

func TestParseBOMAndMiscasedKeys(t *testing.T) {
	ws := t.TempDir()
	path := filepath.Join(ws, ".devcontainer.json")
	data := append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"image": "alpine", "Name": "ignored", "build": {"Dockerfile": "x"}}`)...)
	require.NoError(t, os.WriteFile(path, data, 0o644))

	loaded, err := LoadConfig(ws, "")
	require.NoError(t, err, "a UTF-8 BOM is accepted")
	assert.Equal(t, "alpine", loaded.Config.Image)
	assert.Empty(t, loaded.Config.Name, "property names are case-sensitive")
	assert.Equal(t, PlanTypeImage, loaded.Config.PlanType(), "build.Dockerfile is not build.dockerfile")
	require.Len(t, loaded.Warnings, 2)
	assert.Contains(t, loaded.Warnings[0], `ignoring "Name"`)
	assert.Contains(t, loaded.Warnings[1], `ignoring "build.Dockerfile"`)
}

func TestImageWithBuild(t *testing.T) {
	both, err := Parse([]byte(`{"image": "myorg/dev:latest", "build": {"dockerfile": "Dockerfile"}}`))
	require.NoError(t, err)
	require.NoError(t, Validate(both), "image + build is valid: the built image is tagged as image")
	assert.Equal(t, PlanTypeDockerfile, both.PlanType())

	argsOnly, err := Parse([]byte(`{"image": "myorg/dev:latest", "build": {"args": {"A": "1"}}}`))
	require.NoError(t, err)
	require.NoError(t, Validate(argsOnly))
	assert.Equal(t, PlanTypeImage, argsOnly.PlanType(), "a build without a dockerfile next to image is an image config")

	withCompose, err := Parse([]byte(`{"image": "x", "dockerComposeFile": "c.yml", "service": "app"}`))
	require.NoError(t, err)
	require.Error(t, Validate(withCompose), "compose with image stays ambiguous")
}

func TestWorkspaceMountEmptyAndInvalid(t *testing.T) {
	empty, err := Parse([]byte(`{"image": "x", "workspaceMount": "", "workspaceFolder": "/data"}`))
	require.NoError(t, err)
	require.NotNil(t, empty.WorkspaceMount, `"" is set: no workspace mount`)
	assert.Equal(t, "", *empty.WorkspaceMount)
	require.NoError(t, Validate(empty))

	unset, err := Parse([]byte(`{"image": "x"}`))
	require.NoError(t, err)
	assert.Nil(t, unset.WorkspaceMount)

	bad, err := Parse([]byte(`{"image": "x", "workspaceMount": "type=bind,nonsense"}`))
	require.NoError(t, err)
	err = Validate(bad)
	require.Error(t, err, "an invalid workspaceMount is an error, not a silent default")
	assert.Contains(t, err.Error(), "workspaceMount")
}

func TestBuildPlanPaths(t *testing.T) {
	configPath := "/ws/.devcontainer/devcontainer.json"
	plan := func(build string) *DockerfilePlan {
		cfg, err := Parse([]byte(`{"build": ` + build + `}`))
		require.NoError(t, err)
		return buildPlan(cfg, configPath, "").(*DockerfilePlan)
	}

	p := plan(`{"dockerfile": "../docker/dev.Dockerfile"}`)
	assert.Equal(t, "/ws/docker/dev.Dockerfile", p.Dockerfile)
	assert.Equal(t, "/ws/docker", p.Context, "no context: the Dockerfile's directory")

	p = plan(`{"dockerfile": "Dockerfile", "context": ".."}`)
	assert.Equal(t, "/ws/.devcontainer/Dockerfile", p.Dockerfile)
	assert.Equal(t, "/ws", p.Context)

	p = plan(`{"dockerfile": "/abs/Dockerfile", "context": "/home/u/proj"}`)
	assert.Equal(t, "/abs/Dockerfile", p.Dockerfile, "absolute paths are kept")
	assert.Equal(t, "/home/u/proj", p.Context)

	p = plan(`{"context": ".."}`)
	assert.Equal(t, "/ws/Dockerfile", p.Dockerfile, "no dockerfile: <context>/Dockerfile")
}

func TestImageMetadataIsReadTolerantly(t *testing.T) {
	configs, warnings, err := parseImageMetadata(`[
		{"remoteUser": "dev", "init": "true", "postCreateCommand": "echo hi"},
		"not an object",
		{"containerEnv": {"A": 1}}
	]`, nil)
	require.NoError(t, err)
	require.Len(t, configs, 2)
	assert.Equal(t, "dev", configs[0].RemoteUser, "one bad property does not drop the entry")
	assert.Nil(t, configs[0].Init)
	assert.NotNil(t, configs[0].PostCreateCommand)
	assert.Equal(t, "1", configs[1].ContainerEnv["A"])
	assert.Len(t, warnings, 2)
}
