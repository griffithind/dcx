package service

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const dockerfileConfig = `{"name": "prep", "build": {"dockerfile": "Dockerfile"}, "containerEnv": {"V": "%s"}}`

// A recreate prepares everything before it removes the old container and
// creates from that, so it builds once and a failure leaves the old one
// running (review of ff9134a, P1).
func TestRecreateBuildsOnceBeforeRemoving(t *testing.T) {
	h := newUpHarness(t, strings.Replace(dockerfileConfig, "%s", "1", 1))
	require.NoError(t, os.WriteFile(filepath.Join(h.workspace, ".devcontainer", "Dockerfile"), []byte("FROM alpine:3\n"), 0o644))
	require.NoError(t, h.up(UpOptions{}))
	first := len(h.engine.Calls())

	h.writeConfig(strings.Replace(dockerfileConfig, "%s", "2", 1)) // an edit: recreate
	require.NoError(t, h.up(UpOptions{}))
	calls := h.engine.Calls()[first:]
	removed := slices.IndexFunc(calls, func(c containertest.Call) bool { return c.Method == "RemoveContainer" })
	require.GreaterOrEqual(t, removed, 0, "%v", calls)
	builds := 0
	for i, c := range calls {
		if c.Method == "BuildImage" || c.Method == "PullImageWithProgress" {
			builds++
			assert.Less(t, i, removed, "everything is built before the old container goes: %v", calls)
		}
	}
	assert.Equal(t, 1, builds, "the Dockerfile is built once: %v", calls)
	assert.Len(t, h.containers(), 1)
}

// --pull pulls the base image before resolution, so users, hooks and
// labels come from the image that runs (review of ff9134a, P1).
func TestPullUsesThePulledImagesMetadata(t *testing.T) {
	h := newUpHarness(t, `{"name": "pull", "image": "alpine:3", "updateRemoteUserUID": false}`)
	meta := func(user string) map[string]string {
		return map[string]string{devcontainer.DevcontainerMetadataLabel: `[{"remoteUser":"` + user + `"}]`}
	}
	h.engine.Images = map[string]bool{"alpine:3": true}
	h.engine.LocalImageLabels = map[string]map[string]string{"alpine:3": meta("old")}
	h.engine.Registry = map[string]map[string]string{"alpine:3": meta("new")}

	require.NoError(t, h.up(UpOptions{Pull: true}))
	require.Len(t, h.engine.Specs, 1)
	label := h.engine.Specs[0].Labels[devcontainer.DevcontainerMetadataLabel]
	assert.Contains(t, label, `"remoteUser":"new"`)
	assert.NotContains(t, label, `"remoteUser":"old"`)
	assert.Len(t, h.engine.CallsTo("PullImageWithProgress"), 1, "pulled once, for resolution")
	assert.Equal(t, "sha256:alpine:3", h.engine.Specs[0].Image, "the container runs the prepared image ID")
}

// dcx build gets the build secrets dcx up gets (review of ff9134a, P2).
func TestBuildPassesBuildSecrets(t *testing.T) {
	h := newUpHarness(t, `{"name": "sec", "build": {"dockerfile": "Dockerfile"},
		"customizations": {"dcx": {"buildSecrets": {"tok": "echo s3cret"}}}}`)
	require.NoError(t, os.WriteFile(filepath.Join(h.workspace, ".devcontainer", "Dockerfile"), []byte("FROM alpine:3\n"), 0o644))
	svc := NewDevContainerService(h.engine, report.Discard, h.workspace, "", false)
	require.NoError(t, svc.Build(context.Background(), BuildOptions{}))
	require.NotEmpty(t, h.engine.Builds)
	assert.Contains(t, h.engine.Builds[0].Secrets, "tok")
}
