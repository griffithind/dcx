// Package main provides the entry point for dcx-agent.
// This is a minimal binary that runs inside containers to provide SSH server
// and agent proxy functionality.
package main

import (
	"os"

	"github.com/griffithind/dcx/internal/ssh/server"
)

func main() {
	if err := server.Execute(); err != nil {
		os.Exit(1)
	}
}
