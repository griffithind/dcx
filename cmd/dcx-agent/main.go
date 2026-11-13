// Package main provides the entry point for dcx-agent.
// It runs inside dcx containers: the SSH server (sessions, SFTP, agent and
// port forwarding), plus the setup and ping helpers dcx calls.
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
