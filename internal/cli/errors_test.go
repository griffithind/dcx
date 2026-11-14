package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandsFailClearlyOnBadConfigs runs up and build over broken
// workspaces: each must fail, and say what is wrong. (A missing Dockerfile
// is reported by docker build, so test/e2e covers it: initializeCommand
// may create the Dockerfile after resolution.)
func TestCommandsFailClearlyOnBadConfigs(t *testing.T) {
	commands := map[string]func(*cobra.Command, []string) error{"up": runUp, "build": runBuild}
	for _, tc := range []struct {
		name, config string // config "" means no devcontainer.json at all
		want         string
	}{
		{"missing config", "", "devcontainer.json"},
		{"invalid JSON", "{\n\t\"name\": \"x\"\n\t\"image\": \"alpine:3\"\n}", "devcontainer.json"},
		{"image that cannot be pulled", `{"image": "registry.invalid/nope:v999"}`, "registry.invalid/nope:v999"},
		{"compose with image", `{"image": "alpine:3", "dockerComposeFile": "c.yml", "service": "app"}`, "dockerComposeFile cannot be combined with image or build"},
	} {
		for name, run := range commands {
			t.Run(tc.name+"/"+name, func(t *testing.T) {
				withFakeEngine(t, &containertest.Fake{}, tc.config)
				if tc.config == "" {
					require.NoError(t, os.RemoveAll(filepath.Join(workspacePath, ".devcontainer")))
				}
				var err error
				captureUI(t, func() { err = run(upCmd, nil) })
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.want)
			})
		}
	}
}
