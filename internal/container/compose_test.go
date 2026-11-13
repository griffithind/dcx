package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForRemovalDropsTheDCXOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	override, err := ComposeOverridePath("ws1")
	require.NoError(t, err)
	p := ComposeProject{Name: "proj", Dir: "/w", Files: []string{"/w/compose.yml", override}}
	assert.Equal(t, []string{"/w/compose.yml"}, p.ForRemoval("ws1").Files)
}
