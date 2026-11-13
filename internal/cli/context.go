package cli

import (
	"context"
	"fmt"
	"github.com/griffithind/dcx/internal/ui"
	"os"
	"os/signal"
	"syscall"

	"github.com/griffithind/dcx/internal/container"
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

	// Identifiers contains the workspace identifiers (project name, workspace ID, etc.).
	Identifiers *service.Identifiers

	stop context.CancelFunc
}

// NewCLIContext creates and initializes a CLIContext with Docker client,
// service, and identifiers. The caller must call Close() when done.
func NewCLIContext() (*CLIContext, error) {
	// Ctrl-C / SIGTERM cancel the operation: hooks and remote commands are
	// stopped and Up returns early. Every step of Up is safe to run again.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	docker, err := container.NewDocker()
	if err != nil {
		stop()
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Create service
	svc := service.NewDevContainerService(docker, ui.Reporter(), workspacePath, configPath, verbose)

	// Get identifiers
	ids := svc.Identifiers()

	return &CLIContext{
		stop:        stop,
		Ctx:         ctx,
		Docker:      docker,
		Service:     svc,
		Identifiers: ids,
	}, nil
}

// Close releases resources held by the CLIContext.
// Always call this when done, typically with defer.
func (c *CLIContext) Close() {
	if c.stop != nil {
		c.stop()
	}

}

// GetState retrieves the current container state.
func (c *CLIContext) GetState() (state.ContainerState, *state.ContainerInfo, error) {
	return c.Service.GetStateManager().GetState(c.Ctx, c.Identifiers.WorkspaceID)
}

// WorkspacePath returns the current workspace path.
func (c *CLIContext) WorkspacePath() string {
	return workspacePath
}

// ConfigPath returns the current config path.
func (c *CLIContext) ConfigPath() string {
	return configPath
}
