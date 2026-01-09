package container

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// ComposeClient wraps Docker Compose CLI operations.
type ComposeClient struct {
	projectName string
	files       []string
	workDir     string
}

// NewComposeClient creates a new compose client.
func NewComposeClient(projectName string, files []string, workDir string) *ComposeClient {
	return &ComposeClient{
		projectName: projectName,
		files:       files,
		workDir:     workDir,
	}
}

// Up starts compose services.
func (c *ComposeClient) Up(ctx context.Context, services []string, build bool) error {
	args := c.baseArgs()
	args = append(args, "up", "-d")
	if build {
		args = append(args, "--build")
	}
	args = append(args, services...)
	return c.run(ctx, args...)
}

// Down stops and removes compose services.
func (c *ComposeClient) Down(ctx context.Context, removeVolumes, removeOrphans bool) error {
	args := c.baseArgs()
	args = append(args, "down")
	if removeVolumes {
		args = append(args, "-v")
	}
	if removeOrphans {
		args = append(args, "--remove-orphans")
	}
	return c.run(ctx, args...)
}

// Start starts existing compose services.
func (c *ComposeClient) Start(ctx context.Context, services []string) error {
	args := c.baseArgs()
	args = append(args, "start")
	args = append(args, services...)
	return c.run(ctx, args...)
}

// Stop stops compose services.
func (c *ComposeClient) Stop(ctx context.Context, services []string) error {
	args := c.baseArgs()
	args = append(args, "stop")
	args = append(args, services...)
	return c.run(ctx, args...)
}

// Build builds compose service images.
func (c *ComposeClient) Build(ctx context.Context, services []string, noCache, pull bool) error {
	args := c.baseArgs()
	args = append(args, "build")
	if noCache {
		args = append(args, "--no-cache")
	}
	if pull {
		args = append(args, "--pull")
	}
	args = append(args, services...)
	return c.run(ctx, args...)
}

// PS lists compose containers.
func (c *ComposeClient) PS(ctx context.Context) (string, error) {
	args := c.baseArgs()
	args = append(args, "ps", "--format", "json")
	return c.runOutput(ctx, args...)
}

// baseArgs returns the base docker compose arguments.
func (c *ComposeClient) baseArgs() []string {
	args := []string{}
	if c.projectName != "" {
		args = append(args, "-p", c.projectName)
	}
	for _, f := range c.files {
		args = append(args, "-f", f)
	}
	return args
}

// run executes a docker compose command.
func (c *ComposeClient) run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = c.workDir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker compose %s failed: %w\n%s",
			strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

// runOutput executes a docker compose command and returns output.
func (c *ComposeClient) runOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = c.workDir

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("docker compose %s failed: %w",
			strings.Join(args, " "), err)
	}
	return string(output), nil
}
