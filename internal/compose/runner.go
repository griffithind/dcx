// Package compose provides Docker Compose CLI integration.
package compose

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
)

// Runner manages docker compose operations.
type Runner struct {
	workspacePath  string
	configPath     string
	configDir      string
	cfg            *config.DevcontainerConfig
	envKey         string
	configHash     string
	composeProject string
	composeFiles   []string
	overridePath   string
	derivedImage   string // Derived image with features (if any)
}

// NewRunner creates a new compose runner.
func NewRunner(workspacePath, configPath string, cfg *config.DevcontainerConfig, envKey, configHash string) (*Runner, error) {
	configDir := filepath.Dir(configPath)

	// Resolve compose files
	composeFiles, err := config.ResolveComposeFiles(cfg, configDir)
	if err != nil {
		return nil, err
	}

	// Generate compose project name
	composeProject := "dcx_" + envKey

	return &Runner{
		workspacePath:  workspacePath,
		configPath:     configPath,
		configDir:      configDir,
		cfg:            cfg,
		envKey:         envKey,
		configHash:     configHash,
		composeProject: composeProject,
		composeFiles:   composeFiles,
	}, nil
}

// NewRunnerFromEnvKey creates a runner for an existing environment.
func NewRunnerFromEnvKey(workspacePath, envKey string) *Runner {
	composeProject := "dcx_" + envKey
	return &Runner{
		workspacePath:  workspacePath,
		envKey:         envKey,
		composeProject: composeProject,
	}
}

// writeOverrideToTempFile writes the override content to a temp file and returns the path.
// The caller is responsible for cleaning up the file after use.
func (r *Runner) writeOverrideToTempFile(content string) (string, error) {
	tmpFile, err := os.CreateTemp("", "dcx-override-*.yml")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write override: %w", err)
	}

	tmpFile.Close()
	return tmpFile.Name(), nil
}

// UpOptions contains options for compose up.
type UpOptions struct {
	Build   bool
	Verbose bool
}

// Up runs docker compose up with the generated override file.
func (r *Runner) Up(ctx context.Context, opts UpOptions) error {
	// Check if features are configured
	hasFeatures := len(r.cfg.Features) > 0

	if hasFeatures {
		// Build derived image with features
		if err := r.buildDerivedImageWithFeatures(ctx, opts); err != nil {
			return fmt.Errorf("failed to build derived image with features: %w", err)
		}
	}

	// Generate override file
	override, err := r.generateOverride()
	if err != nil {
		return fmt.Errorf("failed to generate override: %w", err)
	}

	// Write override to temp file
	r.overridePath, err = r.writeOverrideToTempFile(override)
	if err != nil {
		return err
	}
	defer os.Remove(r.overridePath)

	// Build compose args
	args := r.composeBaseArgs()
	args = append(args, "up", "-d")

	// Add --build only if explicitly requested and we DON'T have features.
	// When we have features, we've already built the derived image separately,
	// and using --build would cause compose to rebuild from the base Dockerfile
	// and overwrite our feature image.
	// Non-primary services with build configs will still be built by compose
	// automatically since they don't have an image override.
	if opts.Build && !hasFeatures {
		args = append(args, "--build")
	}

	// Run compose up
	return r.runCompose(ctx, args, opts.Verbose)
}

// buildDerivedImageWithFeatures builds a derived image with features installed.
func (r *Runner) buildDerivedImageWithFeatures(ctx context.Context, opts UpOptions) error {
	if opts.Verbose {
		fmt.Println("Building derived image with features...")
	}

	// Get base image from compose file
	baseImage, err := r.getBaseImage(ctx, opts)
	if err != nil {
		return fmt.Errorf("failed to determine base image: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Base image: %s\n", baseImage)
	}

	// Create feature manager
	mgr, err := features.NewManager(r.configDir)
	if err != nil {
		return fmt.Errorf("failed to create feature manager: %w", err)
	}

	// Resolve and order features
	resolvedFeatures, err := mgr.ResolveAll(ctx, r.cfg.Features, r.cfg.OverrideFeatureInstallOrder)
	if err != nil {
		return fmt.Errorf("failed to resolve features: %w", err)
	}

	if opts.Verbose {
		fmt.Printf("Resolved %d features:\n", len(resolvedFeatures))
		for _, f := range resolvedFeatures {
			name := f.ID
			if f.Metadata != nil && f.Metadata.Name != "" {
				name = f.Metadata.Name
			}
			fmt.Printf("  - %s\n", name)
		}
	}

	// Determine derived image tag
	derivedTag := features.GetDerivedImageTag(r.envKey, r.configHash)

	// Create build directory
	buildDir := filepath.Join(os.TempDir(), "dcx-features", r.envKey)
	defer os.RemoveAll(buildDir)

	if opts.Verbose {
		fmt.Printf("Building derived image: %s\n", derivedTag)
	}

	// Build the derived image
	if err := mgr.BuildDerivedImage(ctx, baseImage, derivedTag, resolvedFeatures, buildDir, opts.Verbose); err != nil {
		return err
	}

	// Store the derived image for use in override
	r.derivedImage = derivedTag

	return nil
}

// getBaseImage determines the base image for the primary service.
func (r *Runner) getBaseImage(ctx context.Context, opts UpOptions) (string, error) {
	// Parse compose files
	compose, err := ParseComposeFiles(r.composeFiles)
	if err != nil {
		return "", fmt.Errorf("failed to parse compose files: %w", err)
	}

	// Get primary service
	serviceName := r.cfg.Service
	if serviceName == "" {
		return "", fmt.Errorf("no primary service specified")
	}

	// Check if service has an image directly
	baseImage, err := compose.GetServiceBaseImage(serviceName)
	if err != nil {
		return "", err
	}

	if baseImage != "" {
		return baseImage, nil
	}

	// Service has a build configuration - we need to build it first
	if compose.HasBuild(serviceName) {
		if opts.Verbose {
			fmt.Println("Building base image from compose...")
		}

		// Run compose build for the service
		buildArgs := r.composeBaseArgs()
		buildArgs = append(buildArgs, "build")

		// Add SSH agent forwarding for build if available
		if ssh.IsAgentAvailable() {
			buildArgs = append(buildArgs, "--ssh", "default")
		}

		buildArgs = append(buildArgs, serviceName)

		if err := r.runCompose(ctx, buildArgs, opts.Verbose); err != nil {
			return "", fmt.Errorf("failed to build service: %w", err)
		}

		// The built image name follows compose naming convention
		// Format: <project>-<service>:latest or <project>_<service>:latest
		return fmt.Sprintf("%s-%s", r.composeProject, serviceName), nil
	}

	return "", fmt.Errorf("could not determine base image for service %q", serviceName)
}

// BuildOptions contains options for compose build.
type BuildOptions struct {
	NoCache bool
	Verbose bool
}

// Build builds images without starting containers.
func (r *Runner) Build(ctx context.Context, opts BuildOptions) error {
	// Generate override file if we have config
	if r.cfg != nil {
		override, err := r.generateOverride()
		if err != nil {
			return fmt.Errorf("failed to generate override: %w", err)
		}

		// Write override to temp file
		r.overridePath, err = r.writeOverrideToTempFile(override)
		if err != nil {
			return err
		}
		defer os.Remove(r.overridePath)
	}

	args := r.composeBaseArgs()
	args = append(args, "build")

	if opts.NoCache {
		args = append(args, "--no-cache")
	}

	// Add SSH agent forwarding for build if available
	if ssh.IsAgentAvailable() {
		args = append(args, "--ssh", "default")
	}

	return r.runCompose(ctx, args, opts.Verbose)
}

// StartOptions contains options for compose start.
type StartOptions struct {
	Verbose bool
}

// Start starts existing containers.
func (r *Runner) Start(ctx context.Context, opts StartOptions) error {
	args := []string{
		"-p", r.composeProject,
		"start",
	}
	return r.runCompose(ctx, args, opts.Verbose)
}

// StopOptions contains options for compose stop.
type StopOptions struct {
	Verbose bool
}

// Stop stops running containers.
func (r *Runner) Stop(ctx context.Context, opts StopOptions) error {
	args := []string{
		"-p", r.composeProject,
		"stop",
	}
	return r.runCompose(ctx, args, opts.Verbose)
}

// DownOptions contains options for compose down.
type DownOptions struct {
	RemoveVolumes bool
	RemoveOrphans bool
	Verbose       bool
}

// Down stops and removes containers.
func (r *Runner) Down(ctx context.Context, opts DownOptions) error {
	args := []string{
		"-p", r.composeProject,
		"down",
	}

	if opts.RemoveVolumes {
		args = append(args, "-v")
	}
	if opts.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}

	return r.runCompose(ctx, args, opts.Verbose)
}

// composeBaseArgs returns the base arguments for compose commands.
func (r *Runner) composeBaseArgs() []string {
	args := []string{"-p", r.composeProject}

	// Add compose files
	for _, f := range r.composeFiles {
		args = append(args, "-f", f)
	}

	// Add override file if it exists
	if r.overridePath != "" {
		args = append(args, "-f", r.overridePath)
	}

	return args
}

// runCompose executes a docker compose command.
func (r *Runner) runCompose(ctx context.Context, args []string, verbose bool) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"compose"}, args...)...)
	cmd.Dir = r.workspacePath

	var stdout, stderr bytes.Buffer
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}

	if err := cmd.Run(); err != nil {
		if !verbose {
			return fmt.Errorf("compose failed: %w\nstderr: %s", err, stderr.String())
		}
		return fmt.Errorf("compose failed: %w", err)
	}

	return nil
}

// generateOverride generates the dcx override compose file.
func (r *Runner) generateOverride() (string, error) {
	gen := &overrideGenerator{
		cfg:            r.cfg,
		envKey:         r.envKey,
		configHash:     r.configHash,
		composeProject: r.composeProject,
		workspacePath:  r.workspacePath,
		derivedImage:   r.derivedImage,
	}
	return gen.Generate()
}

// GetContainerWorkspaceFolder returns the workspace folder path in the container.
func (r *Runner) GetContainerWorkspaceFolder() string {
	return config.DetermineContainerWorkspaceFolder(r.cfg, r.workspacePath)
}

// GetPrimaryService returns the primary service name.
func (r *Runner) GetPrimaryService() string {
	if r.cfg != nil {
		return r.cfg.Service
	}
	return ""
}

// GetComposeProject returns the compose project name.
func (r *Runner) GetComposeProject() string {
	return r.composeProject
}

// Cleanup removes generated files.
func (r *Runner) Cleanup() error {
	if r.overridePath != "" {
		return os.Remove(r.overridePath)
	}
	return nil
}

// ComputeWorkspaceRootHash computes the hash of the workspace root.
func ComputeWorkspaceRootHash(workspacePath string) string {
	return state.ComputeWorkspaceHash(workspacePath)
}
