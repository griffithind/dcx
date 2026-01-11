package container

import (
	"context"
	"os"
	"os/exec"
)

// Compose provides operations for Docker Compose projects.
// It wraps the Docker Compose CLI with a clean API.
type Compose struct {
	projectName   string
	configDir     string
	composeFiles  []string
	overrideFiles []string
}

// ComposeUpOptions configures the Up operation.
type ComposeUpOptions struct {
	Services []string
	Build    bool
	Detach   bool
	Wait     bool
}

// ComposeDownOptions configures the Down operation.
type ComposeDownOptions struct {
	RemoveVolumes bool
	RemoveOrphans bool
}

// ComposeBuildOptions configures the Build operation.
type ComposeBuildOptions struct {
	NoCache bool
	Pull    bool
}

// ComposeClient returns a Compose instance for the given project.
func ComposeClient(configDir, projectName string) *Compose {
	return &Compose{
		projectName: projectName,
		configDir:   configDir,
	}
}

// WithFiles sets the compose files to use.
func (c *Compose) WithFiles(files ...string) *Compose {
	c.composeFiles = files
	return c
}

// WithOverride adds an override file.
func (c *Compose) WithOverride(path string) *Compose {
	c.overrideFiles = append(c.overrideFiles, path)
	return c
}

// Up starts the compose services.
func (c *Compose) Up(ctx context.Context, opts ComposeUpOptions) error {
	args := c.baseArgs()
	args = append(args, "up")

	if opts.Detach {
		args = append(args, "-d")
	}
	if opts.Build {
		args = append(args, "--build")
	}
	if opts.Wait {
		args = append(args, "--wait")
	}

	args = append(args, opts.Services...)

	return c.run(ctx, args)
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

// Build builds or rebuilds services.
func (c *Compose) Build(ctx context.Context, opts ComposeBuildOptions) error {
	args := c.baseArgs()
	args = append(args, "build")

	if opts.NoCache {
		args = append(args, "--no-cache")
	}
	if opts.Pull {
		args = append(args, "--pull")
	}

	return c.run(ctx, args)
}

// Config validates and returns the compose configuration.
func (c *Compose) Config(ctx context.Context) ([]byte, error) {
	args := c.baseArgs()
	args = append(args, "config")

	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	if c.configDir != "" {
		cmd.Dir = c.configDir
	}

	return cmd.Output()
}

// baseArgs returns the base arguments for compose commands.
func (c *Compose) baseArgs() []string {
	args := []string{}

	if c.projectName != "" {
		args = append(args, "-p", c.projectName)
	}

	for _, f := range c.composeFiles {
		args = append(args, "-f", f)
	}

	for _, f := range c.overrideFiles {
		args = append(args, "-f", f)
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
