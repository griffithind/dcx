package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/state"
)

// CLIContext holds initialized resources for CLI commands.
// It consolidates the common initialization pattern used across commands.
type CLIContext struct {
	// Ctx is the context for the operation.
	Ctx context.Context

	// DockerClient is the initialized Docker client.
	DockerClient *container.DockerClient

	// Service is the devcontainer service for devcontainer operations.
	Service *service.DevContainerService

	// Identifiers contains the workspace identifiers (project name, workspace ID, etc.).
	Identifiers *service.Identifiers
}

// NewCLIContext creates and initializes a CLIContext with Docker client,
// service, and identifiers. The caller must call Close() when done.
func NewCLIContext() (*CLIContext, error) {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := container.NewDockerClient()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Docker: %w", err)
	}

	// Create service
	svc := service.NewDevContainerService(dockerClient, workspacePath, configPath, verbose)

	// Get identifiers
	ids, err := svc.GetIdentifiers()
	if err != nil {
		_ = dockerClient.Close()
		svc.Close()
		return nil, fmt.Errorf("failed to get identifiers: %w", err)
	}

	return &CLIContext{
		Ctx:          ctx,
		DockerClient: dockerClient,
		Service:      svc,
		Identifiers:  ids,
	}, nil
}

// Close releases resources held by the CLIContext.
// Always call this when done, typically with defer.
func (c *CLIContext) Close() {
	if c.Service != nil {
		c.Service.Close()
	}
	if c.DockerClient != nil {
		_ = c.DockerClient.Close()
	}
}

// GetState retrieves the current container state.
func (c *CLIContext) GetState() (state.ContainerState, *state.ContainerInfo, error) {
	return c.Service.GetStateManager().GetStateWithProject(
		c.Ctx,
		c.Identifiers.ProjectName,
		c.Identifiers.WorkspaceID,
	)
}

// WorkspacePath returns the current workspace path.
func (c *CLIContext) WorkspacePath() string {
	return workspacePath
}

// ConfigPath returns the current config path.
func (c *CLIContext) ConfigPath() string {
	return configPath
}

// IsVerbose returns whether verbose mode is enabled.
func (c *CLIContext) IsVerbose() bool {
	return verbose
}
