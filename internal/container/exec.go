package container

import (
	"bytes"
	"context"
	"io"
	"os/exec"
)

// ExecConfig contains configuration for executing a command in a container.
type ExecConfig struct {
	ContainerID string
	Cmd         []string
	WorkingDir  string
	User        string
	Env         []string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	TTY         bool
	Detach      bool
}

// Exec executes a command in a running container using Docker CLI.
func Exec(ctx context.Context, cfg ExecConfig) (int, error) {
	args := []string{"exec"}

	// TTY mode
	if cfg.TTY {
		args = append(args, "-t")
	}

	// Interactive mode (stdin attached)
	if cfg.Stdin != nil && !cfg.Detach {
		args = append(args, "-i")
	}

	// Detached mode
	if cfg.Detach {
		args = append(args, "-d")
	}

	// User
	if cfg.User != "" {
		args = append(args, "-u", cfg.User)
	}

	// Working directory
	if cfg.WorkingDir != "" {
		args = append(args, "-w", cfg.WorkingDir)
	}

	// Environment variables
	for _, e := range cfg.Env {
		args = append(args, "-e", e)
	}

	// Container ID and command
	args = append(args, cfg.ContainerID)
	args = append(args, cfg.Cmd...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = cfg.Stdin
	cmd.Stdout = cfg.Stdout
	cmd.Stderr = cfg.Stderr

	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

// ExecSimple executes a command in a container and returns the exit code.
// This is a convenience function for simple command execution without output capture.
func ExecSimple(ctx context.Context, containerID string, cmd []string, user string) (int, error) {
	return Exec(ctx, ExecConfig{
		ContainerID: containerID,
		Cmd:         cmd,
		User:        user,
	})
}

// ExecDetached executes a command in a container in the background.
// The command runs detached and this function returns immediately.
func ExecDetached(ctx context.Context, containerID string, cmd []string, user string) error {
	_, err := Exec(ctx, ExecConfig{
		ContainerID: containerID,
		Cmd:         cmd,
		User:        user,
		Detach:      true,
	})
	return err
}

// ExecOutput executes a command in a container and returns the combined output.
func ExecOutput(ctx context.Context, containerID string, cmd []string, user string) (string, int, error) {
	var buf bytes.Buffer
	exitCode, err := Exec(ctx, ExecConfig{
		ContainerID: containerID,
		Cmd:         cmd,
		User:        user,
		Stdout:      &buf,
		Stderr:      &buf,
	})
	return buf.String(), exitCode, err
}

// ExecResult contains the result of a command execution.
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}
