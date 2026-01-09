package container

import (
	"context"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
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
		AttachStdin:  cfg.Stdin != nil,
		AttachStdout: cfg.Stdout != nil,
		AttachStderr: cfg.Stderr != nil,
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
		// In non-TTY mode, we need to demux the streams
		// This is a simplified implementation
		if cfg.Stdout != nil {
			io.Copy(cfg.Stdout, resp.Reader)
		}
	}

	// Get exit code
	inspect, err := cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return -1, err
	}

	return inspect.ExitCode, nil
}

// ExecResult contains the result of a command execution.
type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
}
