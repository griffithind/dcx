package container

import (
	"bytes"
	"context"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
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

// Exec executes a command in a running container.
func Exec(ctx context.Context, cli *client.Client, cfg ExecConfig) (int, error) {
	// Create exec instance
	execConfig := container.ExecOptions{
		AttachStdin:  cfg.Stdin != nil && !cfg.Detach,
		AttachStdout: cfg.Stdout != nil && !cfg.Detach,
		AttachStderr: cfg.Stderr != nil && !cfg.Detach,
		Detach:       cfg.Detach,
		Tty:          cfg.TTY,
		Cmd:          cfg.Cmd,
		WorkingDir:   cfg.WorkingDir,
		User:         cfg.User,
		Env:          cfg.Env,
	}

	execID, err := cli.ContainerExecCreate(ctx, cfg.ContainerID, execConfig)
	if err != nil {
		return -1, err
	}

	// For detached execution, just start and return
	if cfg.Detach {
		err = cli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{
			Detach: true,
		})
		if err != nil {
			return -1, err
		}
		return 0, nil
	}

	// Attach to exec instance
	resp, err := cli.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{
		Tty: cfg.TTY,
	})
	if err != nil {
		return -1, err
	}
	defer resp.Close()

	// Handle I/O
	if cfg.Stdin != nil {
		go func() {
			io.Copy(resp.Conn, cfg.Stdin)
			resp.CloseWrite()
		}()
	}

	if cfg.TTY {
		// In TTY mode, stdout and stderr are combined
		if cfg.Stdout != nil {
			io.Copy(cfg.Stdout, resp.Reader)
		}
	} else {
		// In non-TTY mode, demux the streams using Docker's stdcopy
		if cfg.Stdout != nil || cfg.Stderr != nil {
			stdout := cfg.Stdout
			stderr := cfg.Stderr
			if stdout == nil {
				stdout = io.Discard
			}
			if stderr == nil {
				stderr = io.Discard
			}
			stdcopy.StdCopy(stdout, stderr, resp.Reader)
		}
	}

	// Get exit code
	inspect, err := cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return -1, err
	}

	return inspect.ExitCode, nil
}

// ExecSimple executes a command in a container and returns the exit code.
// This is a convenience function for simple command execution without output capture.
func ExecSimple(ctx context.Context, cli *client.Client, containerID string, cmd []string, user string) (int, error) {
	return Exec(ctx, cli, ExecConfig{
		ContainerID: containerID,
		Cmd:         cmd,
		User:        user,
	})
}

// ExecDetached executes a command in a container in the background.
// The command runs detached and this function returns immediately.
func ExecDetached(ctx context.Context, cli *client.Client, containerID string, cmd []string, user string) error {
	_, err := Exec(ctx, cli, ExecConfig{
		ContainerID: containerID,
		Cmd:         cmd,
		User:        user,
		Detach:      true,
	})
	return err
}

// ExecOutput executes a command in a container and returns the combined output.
func ExecOutput(ctx context.Context, cli *client.Client, containerID string, cmd []string, user string) (string, int, error) {
	var buf bytes.Buffer
	exitCode, err := Exec(ctx, cli, ExecConfig{
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
