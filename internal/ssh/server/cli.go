// Package server provides the CLI for dcx-agent, a minimal binary that runs inside containers.
// It provides the SSH server functionality.
package server

import (
	"flag"
	"fmt"
	"os"
)

// Execute runs the agent CLI.
func Execute() error {
	if len(os.Args) < 2 {
		printUsage()
		return fmt.Errorf("no command specified")
	}

	switch os.Args[1] {
	case "ssh-server":
		return runSSHServerCmd(os.Args[2:])
	case "-h", "--help", "help":
		printUsage()
		return nil
	default:
		printUsage()
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `dcx-agent - DCX agent for container SSH functionality

Usage:
  dcx-agent <command> [flags]

Commands:
  ssh-server  Run SSH server in stdio mode

Use "dcx-agent <command> --help" for more information about a command.
`)
}

// SSH Server command
func runSSHServerCmd(args []string) error {
	fs := flag.NewFlagSet("ssh-server", flag.ContinueOnError)
	user := fs.String("user", "", "User to run as")
	workDir := fs.String("workdir", "/workspace", "Working directory")
	shell := fs.String("shell", "", "Shell to use (auto-detected if empty)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	shellPath := *shell
	if shellPath == "" {
		shellPath = detectShell()
	}

	hostKeyPath := "/tmp/dcx-agent-ssh-hostkey"
	server, err := NewServer(*user, shellPath, *workDir, hostKeyPath)
	if err != nil {
		return err
	}

	return server.Serve()
}

func detectShell() string {
	shells := []string{"/bin/bash", "/bin/zsh", "/bin/sh"}
	for _, shell := range shells {
		if _, err := os.Stat(shell); err == nil {
			return shell
		}
	}
	return "/bin/sh"
}
