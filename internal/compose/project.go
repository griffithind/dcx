package compose

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"gopkg.in/yaml.v3"
)

// LoadOptions configures how to load a compose project.
type LoadOptions struct {
	// Files is the list of compose files to load.
	Files []string

	// WorkDir is the working directory for relative paths.
	WorkDir string

	// ProjectName overrides the default project name.
	ProjectName string

	// Profiles is the list of profiles to enable.
	Profiles []string

	// EnvFiles is the list of additional env files to load.
	EnvFiles []string

	// Environment provides additional environment variables.
	Environment map[string]string

	// Interpolate enables variable interpolation.
	Interpolate bool

	// ResolvePaths resolves relative paths to absolute.
	ResolvePaths bool
}

// LoadProject loads a compose project from files.
func LoadProject(ctx context.Context, opts LoadOptions) (*types.Project, error) {
	// Determine working directory
	workDir := opts.WorkDir
	if workDir == "" && len(opts.Files) > 0 {
		workDir = filepath.Dir(opts.Files[0])
	}
	if workDir == "" {
		var err error
		workDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("failed to get working directory: %w", err)
		}
	}

	// Build project options
	projectOpts := []cli.ProjectOptionsFn{
		cli.WithWorkingDirectory(workDir),
		cli.WithOsEnv,
		cli.WithDotEnv,
		cli.WithInterpolation(opts.Interpolate),
		cli.WithResolvedPaths(opts.ResolvePaths),
	}

	if opts.ProjectName != "" {
		projectOpts = append(projectOpts, cli.WithName(opts.ProjectName))
	}

	if len(opts.Profiles) > 0 {
		projectOpts = append(projectOpts, cli.WithProfiles(opts.Profiles))
	}

	if len(opts.EnvFiles) > 0 {
		projectOpts = append(projectOpts, cli.WithEnvFiles(opts.EnvFiles...))
	}

	// Create project options
	options, err := cli.NewProjectOptions(opts.Files, projectOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create project options: %w", err)
	}

	// Load the project
	project, err := options.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load project: %w", err)
	}

	return project, nil
}

// LoadProjectWithDefaults loads a compose project with sensible defaults.
func LoadProjectWithDefaults(ctx context.Context, files []string, projectName string) (*types.Project, error) {
	return LoadProject(ctx, LoadOptions{
		Files:        files,
		ProjectName:  projectName,
		Interpolate:  true,
		ResolvePaths: true,
	})
}

// GetExplicitProjectName checks if any of the compose files has an explicit "name" field.
// Returns the name if found, empty string otherwise.
// This is useful to distinguish between an explicitly set name and one auto-derived from directory.
func GetExplicitProjectName(files []string) string {
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}

		if name, ok := raw["name"].(string); ok && name != "" {
			return name
		}
	}
	return ""
}

// GetServiceNames returns the names of all services in a project.
func GetServiceNames(project *types.Project) []string {
	if project == nil {
		return nil
	}

	names := make([]string, 0, len(project.Services))
	for name := range project.Services {
		names = append(names, name)
	}
	return names
}

// GetService returns a service by name from the project.
func GetService(project *types.Project, name string) (*types.ServiceConfig, bool) {
	if project == nil {
		return nil, false
	}

	svc, ok := project.Services[name]
	if !ok {
		return nil, false
	}
	return &svc, true
}

// GetServiceImage returns the image for a service.
// If the service uses a build configuration, returns the computed image name.
func GetServiceImage(project *types.Project, serviceName string) string {
	svc, ok := GetService(project, serviceName)
	if !ok {
		return ""
	}

	// If image is specified, use it
	if svc.Image != "" {
		return svc.Image
	}

	// If build is specified, compute the image name
	if svc.Build != nil {
		// Default image name: projectname-servicename
		return fmt.Sprintf("%s-%s", project.Name, serviceName)
	}

	return ""
}

// HasBuildConfig returns true if the service has a build configuration.
func HasBuildConfig(project *types.Project, serviceName string) bool {
	svc, ok := GetService(project, serviceName)
	if !ok {
		return false
	}
	return svc.Build != nil
}

// GetDependencies returns the services that a service depends on.
func GetDependencies(project *types.Project, serviceName string) []string {
	svc, ok := GetService(project, serviceName)
	if !ok {
		return nil
	}

	deps := make([]string, 0, len(svc.DependsOn))
	for name := range svc.DependsOn {
		deps = append(deps, name)
	}
	return deps
}

// ProjectWithOverrides creates a copy of the project with service overrides applied.
func ProjectWithOverrides(project *types.Project, overrides map[string]ServiceOverride) *types.Project {
	if project == nil || len(overrides) == 0 {
		return project
	}

	// Create a shallow copy
	newProject := *project

	// Apply overrides
	for serviceName, override := range overrides {
		svc, ok := newProject.Services[serviceName]
		if !ok {
			continue
		}

		// Apply image override
		if override.Image != "" {
			svc.Image = override.Image
		}

		// Merge labels
		if len(override.Labels) > 0 {
			if svc.Labels == nil {
				svc.Labels = make(types.Labels)
			}
			for k, v := range override.Labels {
				svc.Labels[k] = v
			}
		}

		// Merge environment
		if len(override.Environment) > 0 {
			if svc.Environment == nil {
				svc.Environment = make(types.MappingWithEquals)
			}
			for k, v := range override.Environment {
				val := v
				svc.Environment[k] = &val
			}
		}

		// Merge ports
		if len(override.Ports) > 0 {
			for _, port := range override.Ports {
				svc.Ports = append(svc.Ports, types.ServicePortConfig{
					Target:    uint32(port.Container),
					Published: fmt.Sprintf("%d", port.Host),
					Protocol:  port.Protocol,
				})
			}
		}

		// Merge volumes
		if len(override.Volumes) > 0 {
			for _, vol := range override.Volumes {
				svc.Volumes = append(svc.Volumes, types.ServiceVolumeConfig{
					Type:     vol.Type,
					Source:   vol.Source,
					Target:   vol.Target,
					ReadOnly: vol.ReadOnly,
				})
			}
		}

		newProject.Services[serviceName] = svc
	}

	return &newProject
}

// ServiceOverride contains overrides for a compose service.
type ServiceOverride struct {
	// Image overrides the service image.
	Image string

	// Labels to add to the service.
	Labels map[string]string

	// Environment variables to add.
	Environment map[string]string

	// Ports to add.
	Ports []PortOverride

	// Volumes to add.
	Volumes []VolumeOverride
}

// PortOverride represents a port to add to a service.
type PortOverride struct {
	Container int
	Host      int
	Protocol  string
}

// VolumeOverride represents a volume to add to a service.
type VolumeOverride struct {
	Type     string
	Source   string
	Target   string
	ReadOnly bool
}
