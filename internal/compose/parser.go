package compose

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
)

// ComposeFile represents a parsed docker-compose.yml file.
// This wraps compose-go's Project type for compatibility.
type ComposeFile struct {
	project *types.Project
	// Keep a map view for backward compatibility
	Services map[string]ServiceConfig
}

// ServiceConfig represents a service configuration in docker-compose.yml.
type ServiceConfig struct {
	Image       string
	Build       *ServiceBuild
	Environment map[string]string
	Volumes     []string
	Ports       []string
	DependsOn   []string
	Command     interface{}
	Entrypoint  interface{}
	// Additional fields from compose-go
	Networks    []string
	HealthCheck *types.HealthCheckConfig
}

// ServiceBuild represents the build configuration for a service.
type ServiceBuild struct {
	Context    string
	Dockerfile string
	Args       map[string]string
	Target     string
	CacheFrom  []string
	SSH        []string
}

// ParseComposeFile parses a docker-compose.yml file using compose-go.
func ParseComposeFile(path string) (*ComposeFile, error) {
	return ParseComposeFiles([]string{path})
}

// ParseComposeFiles parses multiple compose files and merges them using compose-go.
func ParseComposeFiles(paths []string) (*ComposeFile, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no compose files specified")
	}

	ctx := context.Background()

	// Get the directory of the first compose file as working directory
	workDir := filepath.Dir(paths[0])

	// Create project options with compose-go
	options, err := cli.NewProjectOptions(
		paths,
		cli.WithWorkingDirectory(workDir),
		cli.WithOsEnv,
		cli.WithDotEnv,
		cli.WithInterpolation(true),   // Enable variable interpolation
		cli.WithResolvedPaths(true),   // Resolve relative paths
		cli.WithProfiles([]string{}),  // No profile filtering
		cli.WithDiscardEnvFile,        // Don't require env_file to exist (for flexibility)
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create project options: %w", err)
	}

	// Load the project
	project, err := options.LoadProject(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load compose project: %w", err)
	}

	// Convert to our wrapper type
	cf := &ComposeFile{
		project:  project,
		Services: make(map[string]ServiceConfig),
	}

	// Convert services to our format for backward compatibility
	for _, svc := range project.Services {
		cfg := ServiceConfig{
			Image:     svc.Image,
			DependsOn: getDependsOnNames(svc.DependsOn),
		}

		// Convert build config
		if svc.Build != nil {
			cfg.Build = &ServiceBuild{
				Context:    svc.Build.Context,
				Dockerfile: svc.Build.Dockerfile,
				Target:     svc.Build.Target,
			}
			// Convert build args
			if len(svc.Build.Args) > 0 {
				cfg.Build.Args = make(map[string]string)
				for k, v := range svc.Build.Args {
					if v != nil {
						cfg.Build.Args[k] = *v
					}
				}
			}
			// Convert cache_from (strings in compose-go v2)
			cfg.Build.CacheFrom = svc.Build.CacheFrom
			// Convert SSH
			for _, ssh := range svc.Build.SSH {
				cfg.Build.SSH = append(cfg.Build.SSH, ssh.ID)
			}
		}

		// Convert environment
		if len(svc.Environment) > 0 {
			cfg.Environment = make(map[string]string)
			for k, v := range svc.Environment {
				if v != nil {
					cfg.Environment[k] = *v
				}
			}
		}

		// Convert volumes to string format
		for _, vol := range svc.Volumes {
			cfg.Volumes = append(cfg.Volumes, vol.String())
		}

		// Convert ports to string format
		for _, port := range svc.Ports {
			cfg.Ports = append(cfg.Ports, port.Published)
		}

		// Convert command
		if len(svc.Command) > 0 {
			cfg.Command = svc.Command
		}

		// Convert entrypoint
		if len(svc.Entrypoint) > 0 {
			cfg.Entrypoint = svc.Entrypoint
		}

		// Convert networks
		for name := range svc.Networks {
			cfg.Networks = append(cfg.Networks, name)
		}

		// Store healthcheck
		cfg.HealthCheck = svc.HealthCheck

		cf.Services[svc.Name] = cfg
	}

	return cf, nil
}

// getDependsOnNames extracts service names from DependsOn map
func getDependsOnNames(deps types.DependsOnConfig) []string {
	names := make([]string, 0, len(deps))
	for name := range deps {
		names = append(names, name)
	}
	return names
}

// GetProject returns the underlying compose-go Project for advanced usage.
func (c *ComposeFile) GetProject() *types.Project {
	return c.project
}

// GetServiceBaseImage returns the base image for a service.
// For services with an image, returns the image name.
// For services with a build, returns empty string (needs to be built first).
func (c *ComposeFile) GetServiceBaseImage(serviceName string) (string, error) {
	svc, ok := c.Services[serviceName]
	if !ok {
		return "", fmt.Errorf("service %q not found in compose file", serviceName)
	}

	// If service has an image, use it
	if svc.Image != "" {
		return svc.Image, nil
	}

	// If service has a build, we can't determine the base image without building
	if svc.Build != nil {
		return "", nil
	}

	return "", fmt.Errorf("service %q has neither image nor build configuration", serviceName)
}

// GetServiceBuildContext returns the build context for a service.
func (c *ComposeFile) GetServiceBuildContext(serviceName, composeDir string) (string, string, error) {
	svc, ok := c.Services[serviceName]
	if !ok {
		return "", "", fmt.Errorf("service %q not found", serviceName)
	}

	if svc.Build == nil {
		return "", "", nil
	}

	context := svc.Build.Context
	if context == "" {
		context = "."
	}

	// Make context absolute if not already
	if !filepath.IsAbs(context) {
		context = filepath.Join(composeDir, context)
	}

	dockerfile := svc.Build.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}

	return context, dockerfile, nil
}

// HasBuild returns true if the service has a build configuration.
func (c *ComposeFile) HasBuild(serviceName string) bool {
	svc, ok := c.Services[serviceName]
	if !ok {
		return false
	}
	return svc.Build != nil
}

// GetServiceNames returns all service names in the compose file.
func (c *ComposeFile) GetServiceNames() []string {
	names := make([]string, 0, len(c.Services))
	for name := range c.Services {
		names = append(names, name)
	}
	return names
}

// GetServiceNetworks returns the networks a service is connected to.
func (c *ComposeFile) GetServiceNetworks(serviceName string) []string {
	svc, ok := c.Services[serviceName]
	if !ok {
		return nil
	}
	return svc.Networks
}

// GetServiceHealthCheck returns the health check config for a service.
func (c *ComposeFile) GetServiceHealthCheck(serviceName string) *types.HealthCheckConfig {
	svc, ok := c.Services[serviceName]
	if !ok {
		return nil
	}
	return svc.HealthCheck
}
