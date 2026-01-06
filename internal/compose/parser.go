package compose

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ComposeFile represents a parsed docker-compose.yml file.
type ComposeFile struct {
	Version  string                    `yaml:"version,omitempty"`
	Services map[string]ServiceConfig  `yaml:"services"`
}

// ServiceConfig represents a service configuration in docker-compose.yml.
type ServiceConfig struct {
	Image       string            `yaml:"image,omitempty"`
	Build       *ServiceBuild     `yaml:"build,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Volumes     VolumeList        `yaml:"volumes,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
	DependsOn   []string          `yaml:"depends_on,omitempty"`
	Command     interface{}       `yaml:"command,omitempty"`
	Entrypoint  interface{}       `yaml:"entrypoint,omitempty"`
}

// VolumeList represents a list of volume mounts that can be either strings or maps.
type VolumeList []interface{}

// UnmarshalYAML handles both string and map forms of volume configuration.
func (v *VolumeList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("expected sequence, got %v", node.Kind)
	}

	*v = make(VolumeList, 0, len(node.Content))
	for _, item := range node.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			// String form: "host:container" or "volume:container"
			*v = append(*v, item.Value)
		case yaml.MappingNode:
			// Long form: { type: ..., source: ..., target: ... }
			var m map[string]interface{}
			if err := item.Decode(&m); err != nil {
				return err
			}
			*v = append(*v, m)
		default:
			return fmt.Errorf("unexpected volume format: %v", item.Kind)
		}
	}
	return nil
}

// Strings returns only the string-form volumes (for backward compatibility).
func (v VolumeList) Strings() []string {
	var result []string
	for _, vol := range v {
		if s, ok := vol.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// ServiceBuild represents the build configuration for a service.
type ServiceBuild struct {
	Context    string            `yaml:"context,omitempty"`
	Dockerfile string            `yaml:"dockerfile,omitempty"`
	Args       map[string]string `yaml:"args,omitempty"`
	Target     string            `yaml:"target,omitempty"`
}

// UnmarshalYAML handles both string and struct forms of build config.
func (b *ServiceBuild) UnmarshalYAML(node *yaml.Node) error {
	// Handle string form: build: ./context
	if node.Kind == yaml.ScalarNode {
		b.Context = node.Value
		return nil
	}

	// Handle struct form
	type serviceBuildAlias ServiceBuild
	return node.Decode((*serviceBuildAlias)(b))
}

// ParseComposeFile parses a docker-compose.yml file.
func ParseComposeFile(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	return &compose, nil
}

// ParseComposeFiles parses multiple compose files and merges them.
func ParseComposeFiles(paths []string) (*ComposeFile, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no compose files specified")
	}

	// Parse first file
	result, err := ParseComposeFile(paths[0])
	if err != nil {
		return nil, err
	}

	// Merge additional files
	for _, path := range paths[1:] {
		additional, err := ParseComposeFile(path)
		if err != nil {
			return nil, err
		}

		// Merge services
		for name, svc := range additional.Services {
			if existing, ok := result.Services[name]; ok {
				// Merge service configs (additional overrides existing)
				result.Services[name] = mergeServiceConfig(existing, svc)
			} else {
				result.Services[name] = svc
			}
		}
	}

	return result, nil
}

// mergeServiceConfig merges two service configs (b overrides a).
func mergeServiceConfig(a, b ServiceConfig) ServiceConfig {
	result := a

	if b.Image != "" {
		result.Image = b.Image
	}
	if b.Build != nil {
		result.Build = b.Build
	}
	if b.Command != nil {
		result.Command = b.Command
	}
	if b.Entrypoint != nil {
		result.Entrypoint = b.Entrypoint
	}

	// Merge maps
	if b.Environment != nil {
		if result.Environment == nil {
			result.Environment = make(map[string]string)
		}
		for k, v := range b.Environment {
			result.Environment[k] = v
		}
	}

	// Append arrays
	result.Volumes = append(result.Volumes, b.Volumes...)
	result.Ports = append(result.Ports, b.Ports...)
	result.DependsOn = append(result.DependsOn, b.DependsOn...)

	return result
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

	// Make context absolute
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
