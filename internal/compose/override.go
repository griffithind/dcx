package compose

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/parse"
	"github.com/griffithind/dcx/internal/selinux"
	"github.com/griffithind/dcx/internal/state"
	"gopkg.in/yaml.v3"
)

// overrideGenerator generates the dcx compose override file.
type overrideGenerator struct {
	cfg              *config.DevcontainerConfig
	envKey           string
	configHash       string
	composeProject   string
	workspacePath    string
	derivedImage     string             // Derived image to use instead of service's image
	resolvedFeatures []*features.Feature // Resolved features for runtime config
}

// ComposeOverride represents the override file structure.
type ComposeOverride struct {
	Services map[string]ServiceOverride `yaml:"services"`
}

// ServiceOverride represents overrides for a single service.
type ServiceOverride struct {
	Image       string            `yaml:"image,omitempty"`
	PullPolicy  string            `yaml:"pull_policy,omitempty"`
	Entrypoint  []string          `yaml:"entrypoint,omitempty"`
	Command     []string          `yaml:"command,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty"`
	Volumes     []string          `yaml:"volumes,omitempty"`
	WorkingDir  string            `yaml:"working_dir,omitempty"`
	User        string            `yaml:"user,omitempty"`
	CapAdd      []string          `yaml:"cap_add,omitempty"`
	CapDrop     []string          `yaml:"cap_drop,omitempty"`
	SecurityOpt []string          `yaml:"security_opt,omitempty"`
	Privileged  *bool             `yaml:"privileged,omitempty"`
	Init        *bool             `yaml:"init,omitempty"`
	ShmSize     string            `yaml:"shm_size,omitempty"`
	Devices     []string          `yaml:"devices,omitempty"`
	ExtraHosts  []string          `yaml:"extra_hosts,omitempty"`
	NetworkMode string            `yaml:"network_mode,omitempty"`
	IpcMode     string            `yaml:"ipc,omitempty"`
	PidMode     string            `yaml:"pid,omitempty"`
	Ulimits     map[string]Ulimit `yaml:"ulimits,omitempty"`
	Sysctls     map[string]string `yaml:"sysctls,omitempty"`
	Tmpfs       []string          `yaml:"tmpfs,omitempty"`
	Ports       []string          `yaml:"ports,omitempty"`
}

// Ulimit represents a ulimit configuration.
type Ulimit struct {
	Soft int `yaml:"soft"`
	Hard int `yaml:"hard"`
}

// Generate creates the override YAML content.
func (g *overrideGenerator) Generate() (string, error) {
	override := ComposeOverride{
		Services: make(map[string]ServiceOverride),
	}

	// Generate primary service override
	primaryOverride, err := g.generatePrimaryServiceOverride()
	if err != nil {
		return "", err
	}
	override.Services[g.cfg.Service] = primaryOverride

	// Generate overrides for runServices (if any)
	for _, svc := range g.cfg.RunServices {
		if svc != g.cfg.Service {
			runSvcOverride := g.generateRunServiceOverride(svc)
			override.Services[svc] = runSvcOverride
		}
	}

	// Marshal to YAML
	data, err := yaml.Marshal(override)
	if err != nil {
		return "", fmt.Errorf("failed to marshal override: %w", err)
	}

	return string(data), nil
}

// generatePrimaryServiceOverride creates the override for the primary service.
func (g *overrideGenerator) generatePrimaryServiceOverride() (ServiceOverride, error) {
	svc := ServiceOverride{
		Labels:      make(map[string]string),
		Environment: make(map[string]string),
	}

	// Override image if we have a derived image with features
	if g.derivedImage != "" {
		svc.Image = g.derivedImage
		// Set pull_policy to never to prevent compose from rebuilding this service
		// when running with --build. We've already built the image with features installed.
		svc.PullPolicy = "never"
	}

	// Apply overrideCommand if specified (keep container alive instead of running default command)
	if g.cfg.OverrideCommand != nil && *g.cfg.OverrideCommand {
		svc.Entrypoint = []string{"/bin/sh", "-c"}
		svc.Command = []string{"while sleep 1000; do :; done"}
	}

	// Add DCX labels
	g.addLabels(svc.Labels, true)

	// Add workspace mount and working directory
	containerWorkspace := config.DetermineContainerWorkspaceFolder(g.cfg, g.workspacePath)
	svc.WorkingDir = containerWorkspace
	svc.Volumes = append(svc.Volumes, g.formatMount(g.workspacePath, containerWorkspace))

	// Add container/remote environment variables
	for k, v := range g.cfg.ContainerEnv {
		svc.Environment[k] = v
	}
	for k, v := range g.cfg.RemoteEnv {
		svc.Environment[k] = v
	}

	// Add additional mounts from config
	for _, mount := range g.cfg.Mounts {
		parsed := g.parseMountString(mount)
		if parsed != "" {
			svc.Volumes = append(svc.Volumes, parsed)
		}
	}

	// Add mounts from features
	if len(g.resolvedFeatures) > 0 {
		featureMounts := features.CollectMounts(g.resolvedFeatures)
		for _, mount := range featureMounts {
			parsed := g.parseMountString(mount)
			if parsed != "" {
				svc.Volumes = append(svc.Volumes, parsed)
			}
		}
	}

	// Add user override (apply variable substitution for devcontainer variables)
	if g.cfg.RemoteUser != "" {
		svc.User = config.Substitute(g.cfg.RemoteUser, &config.SubstitutionContext{
			LocalWorkspaceFolder: g.workspacePath,
		})
	} else if g.cfg.ContainerUser != "" {
		svc.User = config.Substitute(g.cfg.ContainerUser, &config.SubstitutionContext{
			LocalWorkspaceFolder: g.workspacePath,
		})
	}

	// Map runArgs to compose options
	g.mapRunArgsToService(&svc)

	// Add feature runtime requirements
	if len(g.resolvedFeatures) > 0 {
		// Add capabilities from features
		featureCaps := features.CollectCapabilities(g.resolvedFeatures)
		svc.CapAdd = append(svc.CapAdd, featureCaps...)

		// Add security options from features
		featureSecOpts := features.CollectSecurityOpts(g.resolvedFeatures)
		svc.SecurityOpt = append(svc.SecurityOpt, featureSecOpts...)

		// Check if privileged mode is needed
		if features.NeedsPrivileged(g.resolvedFeatures) {
			t := true
			svc.Privileged = &t
			// Warn user about security implications
			privFeatures := features.GetPrivilegedFeatures(g.resolvedFeatures)
			fmt.Printf("Warning: Enabling privileged mode (requested by features: %s)\n", strings.Join(privFeatures, ", "))
			fmt.Println("  Privileged mode grants full access to host devices and bypasses security features.")
		}

		// Check if init is needed
		if features.NeedsInit(g.resolvedFeatures) {
			t := true
			svc.Init = &t
		}
	}

	return svc, nil
}

// generateRunServiceOverride creates the override for a non-primary runService.
func (g *overrideGenerator) generateRunServiceOverride(serviceName string) ServiceOverride {
	svc := ServiceOverride{
		Labels: make(map[string]string),
	}

	// Add DCX labels (not primary)
	g.addLabels(svc.Labels, false)

	// Add workspace mount
	containerWorkspace := config.DetermineContainerWorkspaceFolder(g.cfg, g.workspacePath)
	svc.Volumes = append(svc.Volumes, g.formatMount(g.workspacePath, containerWorkspace))

	// Set pull_policy to "build" to ensure services with Dockerfiles are rebuilt
	// when config changes. This prevents stale cached images from being used.
	// For services with only an image (no build config), compose will use the image.
	svc.PullPolicy = "build"

	return svc
}

// addLabels adds DCX labels to the service.
func (g *overrideGenerator) addLabels(labels map[string]string, isPrimary bool) {
	labels[docker.LabelManaged] = "true"
	labels[docker.LabelEnvKey] = g.envKey
	labels[docker.LabelWorkspaceRootHash] = state.ComputeWorkspaceHash(g.workspacePath)
	labels[docker.LabelWorkspacePath] = g.workspacePath
	labels[docker.LabelConfigHash] = g.configHash
	labels[docker.LabelPlan] = docker.PlanCompose
	labels[docker.LabelComposeProject] = g.composeProject
	labels[docker.LabelPrimaryService] = g.cfg.Service
	labels[docker.LabelVersion] = docker.LabelSchemaVersion

	if isPrimary {
		labels[docker.LabelPrimary] = "true"
	}
}

// formatMount formats a mount string with SELinux handling.
func (g *overrideGenerator) formatMount(source, target string) string {
	// Check if SELinux is enforcing
	suffix := ""
	if runtime.GOOS == "linux" {
		mode, err := selinux.GetMode()
		if err == nil && mode == selinux.ModeEnforcing {
			suffix = ":Z"
		}
	}

	return fmt.Sprintf("%s:%s%s", source, target, suffix)
}

// parseMountString parses a devcontainer mount string and returns a compose-compatible format.
// Uses the shared parse.ParseMount for consistent parsing.
func (g *overrideGenerator) parseMountString(mount string) string {
	m := parse.ParseMount(mount)
	if m == nil {
		return ""
	}

	// Get SELinux suffix for bind mounts
	suffix := ""
	if m.Type == "bind" && runtime.GOOS == "linux" {
		mode, err := selinux.GetMode()
		if err == nil && mode == selinux.ModeEnforcing {
			suffix = ":Z"
		}
	}

	return m.ToComposeFormat(suffix)
}

// mapRunArgsToService maps devcontainer runArgs to compose service options.
// Uses the shared parse.ParseRunArgs for consistent parsing.
func (g *overrideGenerator) mapRunArgsToService(svc *ServiceOverride) {
	parsed := parse.ParseRunArgs(g.cfg.RunArgs)
	if parsed != nil {
		// Apply parsed values
		svc.CapAdd = append(svc.CapAdd, parsed.CapAdd...)
		svc.CapDrop = append(svc.CapDrop, parsed.CapDrop...)
		svc.SecurityOpt = append(svc.SecurityOpt, parsed.SecurityOpt...)

		if parsed.Privileged {
			t := true
			svc.Privileged = &t
		}
		if parsed.Init {
			t := true
			svc.Init = &t
		}

		svc.ShmSize = parsed.ShmSize
		svc.Devices = append(svc.Devices, parsed.Devices...)
		svc.ExtraHosts = append(svc.ExtraHosts, parsed.ExtraHosts...)
		svc.NetworkMode = parsed.NetworkMode
		svc.IpcMode = parsed.IpcMode
		svc.PidMode = parsed.PidMode
		svc.Tmpfs = append(svc.Tmpfs, parsed.Tmpfs...)
		svc.Ports = append(svc.Ports, parsed.Ports...)

		// Copy sysctls
		if len(parsed.Sysctls) > 0 {
			if svc.Sysctls == nil {
				svc.Sysctls = make(map[string]string)
			}
			for k, v := range parsed.Sysctls {
				svc.Sysctls[k] = v
			}
		}
	}

	// Also check config-level settings
	if g.cfg.Privileged != nil && *g.cfg.Privileged {
		t := true
		svc.Privileged = &t
	}
	if g.cfg.Init != nil && *g.cfg.Init {
		t := true
		svc.Init = &t
	}
	if len(g.cfg.CapAdd) > 0 {
		svc.CapAdd = append(svc.CapAdd, g.cfg.CapAdd...)
	}
	if len(g.cfg.SecurityOpt) > 0 {
		svc.SecurityOpt = append(svc.SecurityOpt, g.cfg.SecurityOpt...)
	}

	// Add forwardPorts from config
	forwardPorts := g.cfg.GetForwardPorts()
	if len(forwardPorts) > 0 {
		svc.Ports = append(svc.Ports, forwardPorts...)
	}
}

