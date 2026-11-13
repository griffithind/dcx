package build

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEngine records builds; containertest.Fake can't be used here because
// it imports this package.
type fakeEngine struct {
	images  map[string]bool
	configs map[string]ImageConfig
	Builds  []DockerfileBuildOptions
}

func (f *fakeEngine) ImageConfig(_ context.Context, ref string) (ImageConfig, error) {
	return f.configs[ref], nil
}

func (f *fakeEngine) BuildImage(_ context.Context, opts DockerfileBuildOptions) error {
	f.Builds = append(f.Builds, opts)
	return nil
}

func (f *fakeEngine) ImageExists(_ context.Context, ref string) (bool, error) {
	return f.images[ref], nil
}

func (f *fakeEngine) ImageLabels(context.Context, string) (map[string]string, error) {
	return nil, nil
}

func TestBuildFromDockerfileResolvesPaths(t *testing.T) {
	fake := &fakeEngine{}
	dir := t.TempDir()
	tag, err := NewBuilder(fake, nil).BuildFromDockerfile(context.Background(), DockerfileBuildOptions{
		Tag: "dcx/ws:base", Context: dir, Dockerfile: "sub/Dockerfile", Args: map[string]string{"A": "1"},
	})
	require.NoError(t, err)
	assert.Equal(t, "dcx/ws:base", tag)
	require.Len(t, fake.Builds, 1)
	assert.Equal(t, dir, fake.Builds[0].Context)
	assert.Equal(t, filepath.Join(dir, "sub/Dockerfile"), fake.Builds[0].Dockerfile, "relative Dockerfile resolves against the context")
}

func TestBuildUIDUpdateUsesCachedImage(t *testing.T) {
	fake := &fakeEngine{images: map[string]bool{"dcx/ws:uid-abc": true}}
	tag, err := NewBuilder(fake, nil).BuildUIDUpdate(context.Background(), UIDBuildOptions{
		BaseImage: "base", Tag: "dcx/ws:uid-abc", RemoteUser: "dev", HostUID: 1000, HostGID: 1000,
	})
	require.NoError(t, err)
	assert.Equal(t, "dcx/ws:uid-abc", tag)
	assert.Empty(t, fake.Builds, "an existing layer is reused")
}

func TestBuildUIDUpdateBuildsWithArgs(t *testing.T) {
	fake := &fakeEngine{}
	_, err := NewBuilder(fake, nil).BuildUIDUpdate(context.Background(), UIDBuildOptions{
		BaseImage: "base:1", Tag: "dcx/ws:uid-abc", RemoteUser: "dev", HostUID: 1001, HostGID: 1002,
	})
	require.NoError(t, err)
	require.Len(t, fake.Builds, 1)
	args := fake.Builds[0].Args
	assert.Equal(t, "base:1", args["BASE_IMAGE"])
	assert.Equal(t, "1001", args["NEW_UID"])
	assert.Equal(t, "1002", args["NEW_GID"])
	assert.Equal(t, "dev", args["IMAGE_USER"], "image user defaults to the remote user")
}
