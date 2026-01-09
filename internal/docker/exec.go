package docker

import (
	"context"
	"fmt"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// ExecConfig contains configuration for exec operations.
type ExecConfig struct {
	Cmd        []string
	Env        []string
	WorkingDir string
	User       string
	Tty        bool
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
}

// Exec runs a command inside a container.
func (c *Client) Exec(ctx context.Context, containerID string, config ExecConfig) (int, error) {
	execConfig := container.ExecOptions{
		Cmd:          config.Cmd,
		Env:          config.Env,
		WorkingDir:   config.WorkingDir,
		User:         config.User,
		Tty:          config.Tty,
		AttachStdin:  config.Stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
	}

	resp, err := c.cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return -1, fmt.Errorf("failed to create exec: %w", err)
	}

	attachResp, err := c.cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{
		Tty: config.Tty,
	})
	if err != nil {
		return -1, fmt.Errorf("failed to attach exec: %w", err)
	}
	defer attachResp.Close()

	// Handle I/O in goroutines for interactive mode
	errCh := make(chan error, 2)

	// Copy stdin if provided
	if config.Stdin != nil {
		go func() {
			_, err := io.Copy(attachResp.Conn, config.Stdin)
			// Close write side to signal EOF
			if cw, ok := attachResp.Conn.(interface{ CloseWrite() error }); ok {
				cw.CloseWrite()
			}
			errCh <- err
		}()
	}

	// Copy output
	go func() {
		if config.Tty {
			// In TTY mode, stdout and stderr are combined
			if config.Stdout != nil {
				_, err := io.Copy(config.Stdout, attachResp.Reader)
				errCh <- err
				return
			}
		} else {
			// Non-TTY mode, demux stdout and stderr
			_, err := StdCopy(config.Stdout, config.Stderr, attachResp.Reader)
			errCh <- err
			return
		}
		errCh <- nil
	}()

	// Wait for output to complete
	<-errCh
	if config.Stdin != nil {
		<-errCh
	}

	// Get exit code
	inspectResp, err := c.cli.ContainerExecInspect(ctx, resp.ID)
	if err != nil {
		return -1, fmt.Errorf("failed to inspect exec: %w", err)
	}

	return inspectResp.ExitCode, nil
}

// StdCopy demultiplexes Docker's multiplexed streams.
// Docker multiplexes stdout and stderr into a single stream when not using a TTY.
// This function properly separates them using the Docker protocol.
func StdCopy(stdout, stderr io.Writer, src io.Reader) (written int64, err error) {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return stdcopy.StdCopy(stdout, stderr, src)
}

// LogsOptions contains options for retrieving container logs.
type LogsOptions struct {
	Follow     bool
	Timestamps bool
	Tail       string // Number of lines or "all"
}

// GetLogs retrieves logs from a container.
func (c *Client) GetLogs(ctx context.Context, containerID string, opts LogsOptions) (io.ReadCloser, error) {
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     opts.Follow,
		Timestamps: opts.Timestamps,
	}

	if opts.Tail != "" && opts.Tail != "all" {
		options.Tail = opts.Tail
	}

	return c.cli.ContainerLogs(ctx, containerID, options)
}

// AttachOptions contains options for attaching to a container.
type AttachOptions struct {
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	TTY        bool
	DetachKeys string
}

// AttachContainer attaches to a running container's streams.
func (c *Client) AttachContainer(ctx context.Context, containerID string, opts AttachOptions) error {
	attachOpts := container.AttachOptions{
		Stream: true,
		Stdin:  opts.Stdin != nil,
		Stdout: true,
		Stderr: true,
	}

	if opts.DetachKeys != "" {
		attachOpts.DetachKeys = opts.DetachKeys
	}

	resp, err := c.cli.ContainerAttach(ctx, containerID, attachOpts)
	if err != nil {
		return fmt.Errorf("failed to attach: %w", err)
	}
	defer resp.Close()

	// Handle I/O
	errCh := make(chan error, 2)

	// Copy stdin if provided
	if opts.Stdin != nil {
		go func() {
			_, err := io.Copy(resp.Conn, opts.Stdin)
			if cw, ok := resp.Conn.(interface{ CloseWrite() error }); ok {
				cw.CloseWrite()
			}
			errCh <- err
		}()
	}

	// Copy output
	go func() {
		if opts.TTY {
			// TTY mode - stdout and stderr are combined
			if opts.Stdout != nil {
				_, err := io.Copy(opts.Stdout, resp.Reader)
				errCh <- err
				return
			}
		} else {
			// Non-TTY mode - demux stdout/stderr
			_, err := StdCopy(opts.Stdout, opts.Stderr, resp.Reader)
			errCh <- err
			return
		}
		errCh <- nil
	}()

	// Wait for output to complete
	<-errCh
	if opts.Stdin != nil {
		<-errCh
	}

	return nil
}
