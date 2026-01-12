package container

import (
	"context"
	"os"
	"os/exec"
)

// Compose provides operations for Docker Compose projects.
// It wraps the Docker Compose CLI with a clean API.
type Compose struct {
	projectName string
	configDir   string
}

// ComposeDownOptions configures the Down operation.
type ComposeDownOptions struct {
	RemoveVolumes bool
	RemoveOrphans bool
}

// ComposeClient returns a Compose instance for the given project.
func ComposeClient(configDir, projectName string) *Compose {
	return &Compose{
		projectName: projectName,
		configDir:   configDir,
	}
}

// Down stops and removes compose services.
func (c *Compose) Down(ctx context.Context, opts ComposeDownOptions) error {
	args := c.baseArgs()
	args = append(args, "down")

	if opts.RemoveVolumes {
		args = append(args, "-v")
	}
	if opts.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}

	return c.run(ctx, args)
}

// Start starts existing stopped services.
func (c *Compose) Start(ctx context.Context) error {
	args := c.baseArgs()
	args = append(args, "start")
	return c.run(ctx, args)
}

// Stop stops running services without removing them.
func (c *Compose) Stop(ctx context.Context) error {
	args := c.baseArgs()
	args = append(args, "stop")
	return c.run(ctx, args)
}

// baseArgs returns the base arguments for compose commands.
func (c *Compose) baseArgs() []string {
	args := []string{}

	if c.projectName != "" {
		args = append(args, "-p", c.projectName)
	}

	return args
}

// run executes a compose command.
func (c *Compose) run(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	if c.configDir != "" {
		cmd.Dir = c.configDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}
