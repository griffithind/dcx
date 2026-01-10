package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCLIContextClose(t *testing.T) {
	// Test that Close handles nil gracefully
	ctx := &CLIContext{}
	assert.NotPanics(t, func() {
		ctx.Close()
	})
}

func TestCLIContextMethods(t *testing.T) {
	// Test accessor methods return expected global values
	ctx := &CLIContext{}

	// These methods access global variables, so we test they don't panic
	assert.NotPanics(t, func() {
		_ = ctx.WorkspacePath()
	})
	assert.NotPanics(t, func() {
		_ = ctx.ConfigPath()
	})
	assert.NotPanics(t, func() {
		_ = ctx.IsVerbose()
	})
}
