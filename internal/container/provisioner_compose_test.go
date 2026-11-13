package container_test

import (
	"context"
	"testing"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// composeUIDRuntime is a compose plan whose image-only primary service gets
// dcx's UID layer (non-root remoteUser), the common case R2/R3 broke.
func composeUIDRuntime(t *testing.T, fake *containertest.Fake) *container.Provisioner {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	resolved := &devcontainer.ResolvedDevContainer{
		Identity:        devcontainer.Identity{WorkspaceID: "ws1234567890", Name: "app", ResourceName: "app-ws123456"},
		Plan:            devcontainer.NewComposePlan([]string{"/w/compose.yml"}, "app", "proj"),
		RemoteUser:      "vscode",
		EffectiveUser:   "vscode",
		ShouldUpdateUID: true,
		HostUID:         1000,
		HostGID:         1000,
		Config:          &devcontainer.DevContainerConfig{},
	}
	prov, err := container.NewProvisioner(fake, nil, resolved)
	require.NoError(t, err)
	return prov
}

func TestComposeImageOnlyBaseIsPulledBeforeLayering(t *testing.T) {
	fake := &containertest.Fake{
		ComposeOutputs: map[string]string{"config --format json": `{"services":{"app":{"image":"mcr.example/base:1"}}}`},
		Registry:       map[string]map[string]string{"mcr.example/base:1": {}},
	}
	require.NoError(t, composeUIDRuntime(t, fake).Up(context.Background(), container.UpOptions{}))
	assert.Len(t, fake.CallsTo("PullImageWithProgress"), 1, "compose build skips image-only services; dcx must pull before layering")
	require.Len(t, fake.Builds, 1, "the UID layer is built")
}

func TestComposeRecreateDoesNotRebuildOverDcxLayers(t *testing.T) {
	fake := &containertest.Fake{
		ComposeOutputs: map[string]string{"config --format json": `{"services":{"app":{"image":"mcr.example/base:1"}}}`},
		Registry:       map[string]map[string]string{"mcr.example/base:1": {}},
	}
	// A recreate passes Build: compose must still not rebuild the service
	// onto dcx's uid-* tag.
	require.NoError(t, composeUIDRuntime(t, fake).Up(context.Background(), container.UpOptions{Build: true, Rebuild: true}))
	var up []string
	for _, c := range fake.CallsTo("Compose") {
		if len(c.Args) > 1 && c.Args[1] == "up" {
			up = c.Args
		}
	}
	require.NotEmpty(t, up)
	assert.NotContains(t, up, "--build")
}

func TestBuildImagesBuildsComposeServicesWithoutLayers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := &containertest.Fake{
		ComposeOutputs: map[string]string{"config --format json": `{"services":{"app":{"build":{"context":"."}}}}`},
	}
	prov, err := container.NewProvisioner(fake, nil, &devcontainer.ResolvedDevContainer{
		Identity: devcontainer.Identity{WorkspaceID: "ws1234567890", ResourceName: "app-ws123456"},
		Plan:     devcontainer.NewComposePlan([]string{"/w/compose.yml"}, "app", "proj"),
		Config:   &devcontainer.DevContainerConfig{},
	})
	require.NoError(t, err)
	require.NoError(t, prov.BuildImages(context.Background(), container.UpOptions{Build: true, Rebuild: true}))
	calls := fake.CallsTo("Compose")
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"proj", "build"}, calls[0].Args, "a recreate builds compose services before the old environment is removed")
}
