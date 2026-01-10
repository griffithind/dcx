// Package main provides the entry point for dcx-agent.
// This is a minimal binary that runs inside containers to provide SSH server
// and agent proxy functionality.
package main

import (
	"os"

	"github.com/griffithind/dcx/internal/agent"
)

func main() {
	if err := agent.Execute(); err != nil {
		os.Exit(1)
	}
}
