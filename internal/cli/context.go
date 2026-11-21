package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/griffithind/dcx/internal/ui"

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

	// Docker is the container engine (the daemon was reachable).
	Docker container.Engine

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

// newEngine is the only place the CLI gets a container engine; tests
// replace it with containertest.Fake. It does not contact the daemon: a
// command that needs Docker reports docker's own error ("Cannot connect to
// the Docker daemon …") from its first operation, and checking first cost
// every command a round trip.
var newEngine = func() container.Engine { return container.NewDockerCLI().WithTrace(traceDocker) }

// traceDocker shows each docker command line with -v, so a failure can be
// understood and reproduced by hand.
func traceDocker(cmdline string) { ui.Debug("$ %s", cmdline) }

// newService builds the DevContainerService for the global workspace and
// config flags. It is the only place the CLI constructs one.
func newService(engine container.Engine, rep report.Reporter) *service.DevContainerService {
	return service.NewDevContainerService(engine, rep, workspacePath, configPath, verbose)
}

// NewCLIContext creates the context, Docker engine, service and
// identifiers for a command that talks to Docker. The caller must call
// Close() when done.
func NewCLIContext() *CLIContext {
	ctx, stop := commandContext()
	docker := newEngine()
	svc := newService(docker, ui.Reporter())
	ids := svc.Identity()

	return &CLIContext{
		stop:     stop,
		Ctx:      ctx,
		Docker:   docker,
		Service:  svc,
		Identity: ids,
	}
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
