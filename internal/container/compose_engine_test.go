package container_test

import (
	"context"
	"testing"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComposeProjectCommandsGoThroughEngine(t *testing.T) {
	fake := &containertest.Fake{}
	p := container.ComposeProject{Name: "proj", Files: []string{"/w/compose.yml"}}
	ctx := context.Background()

	require.NoError(t, p.Stop(ctx, fake))
	require.NoError(t, p.Start(ctx, fake, []string{"app", "db"}))
	require.NoError(t, p.Down(ctx, fake, container.DownOptions{RemoveVolumes: true, RemoveOrphans: true}))

	calls := fake.CallsTo("Compose")
	require.Len(t, calls, 3)
	assert.Equal(t, []string{"proj", "stop"}, calls[0].Args)
	assert.Equal(t, []string{"proj", "up", "-d", "--no-recreate", "app", "db"}, calls[1].Args, "as devcontainers/cli starts it (R16)")
	assert.Equal(t, []string{"proj", "down", "-v", "--remove-orphans"}, calls[2].Args)
}
