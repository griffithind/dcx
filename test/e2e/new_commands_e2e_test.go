//go:build e2e

package e2e

import (
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
)

// TestCompletionCommandE2E tests the dcx completion command (no Docker needed).
func TestCompletionCommandE2E(t *testing.T) {
	t.Parallel()

	t.Run("bash", func(t *testing.T) {
		stdout := helpers.RunDCXSuccess(t, "completion", "bash")
		assert.Contains(t, stdout, "bash completion")
	})

	t.Run("zsh", func(t *testing.T) {
		stdout := helpers.RunDCXSuccess(t, "completion", "zsh")
		assert.Contains(t, stdout, "zsh completion")
	})

	t.Run("fish", func(t *testing.T) {
		stdout := helpers.RunDCXSuccess(t, "completion", "fish")
		assert.Contains(t, stdout, "fish")
	})
}
