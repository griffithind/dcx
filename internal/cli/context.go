package cli

import (
	"context"
	"fmt"
	"github.com/griffithind/dcx/internal/ui"
	"os"
	"os/signal"
	"syscall"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/state"
)

// CLIContext holds initialized resources for CLI commands.
// It consolidates the common initialization pattern used across commands.
type CLIContext struct {
	// Ctx is the context for the operation.
	Ctx context.Context

	// Docker is the initialized Docker client.
	Docker *container.Docker

	// Service is the devcontainer service for devcontainer operations.
	Service *service.DevContainerService

	// Identity names the workspace (ID, resource name, SSH alias).
	Identity *devcontainer.Identity

	stop context.CancelFunc
}

// This file is the CLI's composition root: commands get their context,
// engine and service from here rather than building their own.

// commandContext returns the context for a command: Ctrl-C / SIGTERM
// cancel it, so hooks, builds, feature fetches and remote commands stop
// (every step of Up is safe to run again). Call stop when done.
func commandContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// newService builds the DevContainerService for the global workspace and
// config flags. It is the only place the CLI constructs one.
func newService(engine container.Engine, rep report.Reporter) *service.DevContainerService {
	return service.NewDevContainerService(engine, rep, workspacePath, configPath, verbose)
}

// NewCLIContext creates the context, Docker engine (checking the daemon is
// reachable), service and identifiers for a command that talks to Docker.
// The caller must call Close() when done.
func NewCLIContext() (*CLIContext, error) {
	ctx, stop := commandContext()

	docker, err := container.NewDocker()
	if err != nil {
		stop()
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	svc := newService(docker, ui.Reporter())
	ids := svc.Identity()

	return &CLIContext{
		stop:     stop,
		Ctx:      ctx,
		Docker:   docker,
		Service:  svc,
		Identity: ids,
	}, nil
}

// Close releases resources held by the CLIContext.
// Always call this when done, typically with defer.
func (c *CLIContext) Close() {
	if c.stop != nil {
		c.stop()
	}
}

// State retrieves the current container state.
func (c *CLIContext) State() (state.ContainerState, *state.ContainerInfo, error) {
	return c.Service.StateManager().State(c.Ctx, c.Identity.WorkspaceID)
}
