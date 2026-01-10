// Package compose provides compose orchestration capabilities.
//
// This package uses:
// - compose-go for project loading and parsing (the standard compose file parser)
// - Docker CLI for compose orchestration (battle-tested, stable interface)
// - Docker SDK for direct container operations when needed
//
// Note: The Docker Compose SDK v2 requires the full docker CLI infrastructure
// (command.Cli interface), making it unsuitable for standalone library use.
// This package provides a clean abstraction over the compose CLI while
// maintaining programmatic control.
package compose

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/client"
)

// Orchestrator provides compose orchestration capabilities.
type Orchestrator struct {
	client     *client.Client
	workingDir string
}

// OrchestratorOption configures an Orchestrator.
type OrchestratorOption func(*Orchestrator)

// WithWorkingDir sets the working directory for compose operations.
func WithWorkingDir(dir string) OrchestratorOption {
	return func(o *Orchestrator) {
		o.workingDir = dir
	}
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(dockerClient *client.Client, opts ...OrchestratorOption) (*Orchestrator, error) {
	o := &Orchestrator{
		client: dockerClient,
	}

	for _, opt := range opts {
		opt(o)
	}

	return o, nil
}

// NewOrchestratorFromEnv creates a new Orchestrator using Docker client from environment.
func NewOrchestratorFromEnv(opts ...OrchestratorOption) (*Orchestrator, error) {
	dockerClient, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, err
	}

	return NewOrchestrator(dockerClient, opts...)
}

// Client returns the underlying Docker client.
func (o *Orchestrator) Client() *client.Client {
	return o.client
}

// UpOptions configures the Up operation.
type UpOptions struct {
	// Project is the compose project (used for project name and file paths).
	Project *types.Project

	// ProjectName overrides the project name.
	ProjectName string

	// Files are the compose files to use.
	Files []string

	// Build forces building images before starting.
	Build bool

	// ForceRecreate forces recreating containers.
	ForceRecreate bool

	// NoRecreate prevents recreating containers.
	NoRecreate bool

	// RemoveOrphans removes containers for services not defined in the compose file.
	RemoveOrphans bool

	// Services limits operation to specific services.
	Services []string

	// Detach runs in background (default true).
	Detach bool

	// Wait waits for services to be healthy.
	Wait bool

	// Stdout is the output writer.
	Stdout io.Writer

	// Stderr is the error writer.
	Stderr io.Writer
}

// Up starts compose services.
func (o *Orchestrator) Up(ctx context.Context, opts UpOptions) error {
	args := o.buildBaseArgs(opts.ProjectName, opts.Files, opts.Project)
	args = append(args, "up", "-d")

	if opts.Build {
		args = append(args, "--build")
	}
	if opts.ForceRecreate {
		args = append(args, "--force-recreate")
	}
	if opts.NoRecreate {
		args = append(args, "--no-recreate")
	}
	if opts.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}
	if opts.Wait {
		args = append(args, "--wait")
	}

	args = append(args, opts.Services...)

	return o.runCompose(ctx, args, opts.Stdout, opts.Stderr)
}

// DownOptions configures the Down operation.
type DownOptions struct {
	// Project is the compose project.
	Project *types.Project

	// ProjectName overrides the project name.
	ProjectName string

	// Files are the compose files to use.
	Files []string

	// RemoveOrphans removes containers for services not defined in the compose file.
	RemoveOrphans bool

	// Volumes removes named volumes.
	Volumes bool

	// Images removes images (none, local, all).
	Images string

	// Timeout is the shutdown timeout in seconds.
	Timeout *int

	// Stdout is the output writer.
	Stdout io.Writer

	// Stderr is the error writer.
	Stderr io.Writer
}

// Down stops and removes compose services.
func (o *Orchestrator) Down(ctx context.Context, opts DownOptions) error {
	args := o.buildBaseArgs(opts.ProjectName, opts.Files, opts.Project)
	args = append(args, "down")

	if opts.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}
	if opts.Volumes {
		args = append(args, "-v")
	}
	if opts.Images != "" {
		args = append(args, "--rmi", opts.Images)
	}
	if opts.Timeout != nil {
		args = append(args, "-t", fmt.Sprintf("%d", *opts.Timeout))
	}

	return o.runCompose(ctx, args, opts.Stdout, opts.Stderr)
}

// StartOptions configures the Start operation.
type StartOptions struct {
	// Project is the compose project.
	Project *types.Project

	// ProjectName overrides the project name.
	ProjectName string

	// Files are the compose files to use.
	Files []string

	// Services limits operation to specific services.
	Services []string

	// Stdout is the output writer.
	Stdout io.Writer

	// Stderr is the error writer.
	Stderr io.Writer
}

// Start starts existing compose containers.
func (o *Orchestrator) Start(ctx context.Context, opts StartOptions) error {
	args := o.buildBaseArgs(opts.ProjectName, opts.Files, opts.Project)
	args = append(args, "start")
	args = append(args, opts.Services...)

	return o.runCompose(ctx, args, opts.Stdout, opts.Stderr)
}

// StopOptions configures the Stop operation.
type StopOptions struct {
	// Project is the compose project.
	Project *types.Project

	// ProjectName overrides the project name.
	ProjectName string

	// Files are the compose files to use.
	Files []string

	// Services limits operation to specific services.
	Services []string

	// Timeout is the stop timeout in seconds.
	Timeout *int

	// Stdout is the output writer.
	Stdout io.Writer

	// Stderr is the error writer.
	Stderr io.Writer
}

// Stop stops compose containers.
func (o *Orchestrator) Stop(ctx context.Context, opts StopOptions) error {
	args := o.buildBaseArgs(opts.ProjectName, opts.Files, opts.Project)
	args = append(args, "stop")

	if opts.Timeout != nil {
		args = append(args, "-t", fmt.Sprintf("%d", *opts.Timeout))
	}

	args = append(args, opts.Services...)

	return o.runCompose(ctx, args, opts.Stdout, opts.Stderr)
}

// BuildOptions configures the Build operation.
type BuildOptions struct {
	// Project is the compose project.
	Project *types.Project

	// ProjectName overrides the project name.
	ProjectName string

	// Files are the compose files to use.
	Files []string

	// Services limits operation to specific services.
	Services []string

	// NoCache disables build cache.
	NoCache bool

	// Pull always pulls base images.
	Pull bool

	// Stdout is the output writer.
	Stdout io.Writer

	// Stderr is the error writer.
	Stderr io.Writer
}

// Build builds compose service images.
func (o *Orchestrator) Build(ctx context.Context, opts BuildOptions) error {
	args := o.buildBaseArgs(opts.ProjectName, opts.Files, opts.Project)
	args = append(args, "build")

	if opts.NoCache {
		args = append(args, "--no-cache")
	}
	if opts.Pull {
		args = append(args, "--pull")
	}

	args = append(args, opts.Services...)

	return o.runCompose(ctx, args, opts.Stdout, opts.Stderr)
}

// PsOptions configures the Ps operation.
type PsOptions struct {
	// Project is the compose project.
	Project *types.Project

	// ProjectName overrides the project name.
	ProjectName string

	// Files are the compose files to use.
	Files []string

	// Services limits operation to specific services.
	Services []string

	// All includes stopped containers.
	All bool
}

// ContainerSummary represents a container in the compose project.
type ContainerSummary struct {
	ID      string
	Name    string
	Service string
	State   string
	Status  string
	Ports   string
}

// Ps lists containers for a compose project.
func (o *Orchestrator) Ps(ctx context.Context, opts PsOptions) ([]ContainerSummary, error) {
	args := o.buildBaseArgs(opts.ProjectName, opts.Files, opts.Project)
	args = append(args, "ps", "--format", "json")

	if opts.All {
		args = append(args, "-a")
	}

	args = append(args, opts.Services...)

	// Note: For proper JSON parsing, we'd need to capture and parse output.
	// For now, this returns an empty slice - full implementation requires
	// capturing stdout and parsing JSON.
	if err := o.runCompose(ctx, args, nil, nil); err != nil {
		return nil, err
	}

	return nil, nil
}

// buildBaseArgs builds the base compose command arguments.
func (o *Orchestrator) buildBaseArgs(projectName string, files []string, project *types.Project) []string {
	var args []string

	// Project name
	name := projectName
	if name == "" && project != nil {
		name = project.Name
	}
	if name != "" {
		args = append(args, "-p", name)
	}

	// Files
	if len(files) > 0 {
		for _, f := range files {
			args = append(args, "-f", f)
		}
	} else if project != nil {
		for _, f := range project.ComposeFiles {
			args = append(args, "-f", f)
		}
	}

	return args
}

// runCompose executes a docker compose command.
func (o *Orchestrator) runCompose(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)

	if o.workingDir != "" {
		cmd.Dir = o.workingDir
	}

	if stdout != nil {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = os.Stdout
	}

	if stderr != nil {
		cmd.Stderr = stderr
	} else {
		cmd.Stderr = os.Stderr
	}

	cmd.Stdin = os.Stdin

	return cmd.Run()
}

// RunComposeCommand executes an arbitrary compose command.
// This is useful for commands not directly supported by the Orchestrator.
func (o *Orchestrator) RunComposeCommand(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return o.runCompose(ctx, args, stdout, stderr)
}

// Close closes the orchestrator's resources.
func (o *Orchestrator) Close() error {
	if o.client != nil {
		return o.client.Close()
	}
	return nil
}

// GetComposeVersion returns the docker compose version.
func GetComposeVersion(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "compose", "version", "--short")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get compose version: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// IsComposeAvailable checks if docker compose is available.
func IsComposeAvailable(ctx context.Context) bool {
	_, err := GetComposeVersion(ctx)
	return err == nil
}
