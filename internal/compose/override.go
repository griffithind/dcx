package compose

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/features"
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
func (g *overrideGenerator) parseMountString(mount string) string {
	// Devcontainer mount format: "source=...,target=...,type=bind,consistency=cached"
	parts := strings.Split(mount, ",")

	var source, target, mountType string
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		switch key {
		case "source", "src":
			source = value
		case "target", "dst", "destination":
			target = value
		case "type":
			mountType = value
		}
	}

	// Default type is bind
	if mountType == "" {
		mountType = "bind"
	}

	// For bind mounts, format as source:target
	if mountType == "bind" && source != "" && target != "" {
		return g.formatMount(source, target)
	}

	// For volume mounts, use named volume syntax
	if mountType == "volume" && source != "" && target != "" {
		return fmt.Sprintf("%s:%s", source, target)
	}

	// For tmpfs, return as-is (compose handles this differently)
	if mountType == "tmpfs" && target != "" {
		return fmt.Sprintf("tmpfs:%s", target)
	}

	// Can't parse, return empty
	return ""
}

// mapRunArgsToService maps devcontainer runArgs to compose service options.
func (g *overrideGenerator) mapRunArgsToService(svc *ServiceOverride) {
	for i := 0; i < len(g.cfg.RunArgs); i++ {
		arg := g.cfg.RunArgs[i]

		switch {
		case strings.HasPrefix(arg, "--cap-add="):
			svc.CapAdd = append(svc.CapAdd, strings.TrimPrefix(arg, "--cap-add="))
		case arg == "--cap-add" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.CapAdd = append(svc.CapAdd, g.cfg.RunArgs[i])

		case strings.HasPrefix(arg, "--cap-drop="):
			svc.CapDrop = append(svc.CapDrop, strings.TrimPrefix(arg, "--cap-drop="))
		case arg == "--cap-drop" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.CapDrop = append(svc.CapDrop, g.cfg.RunArgs[i])

		case strings.HasPrefix(arg, "--security-opt="):
			svc.SecurityOpt = append(svc.SecurityOpt, strings.TrimPrefix(arg, "--security-opt="))
		case arg == "--security-opt" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.SecurityOpt = append(svc.SecurityOpt, g.cfg.RunArgs[i])

		case arg == "--privileged":
			t := true
			svc.Privileged = &t

		case arg == "--init":
			t := true
			svc.Init = &t

		case strings.HasPrefix(arg, "--shm-size="):
			svc.ShmSize = strings.TrimPrefix(arg, "--shm-size=")
		case arg == "--shm-size" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.ShmSize = g.cfg.RunArgs[i]

		case strings.HasPrefix(arg, "--device="):
			svc.Devices = append(svc.Devices, strings.TrimPrefix(arg, "--device="))
		case arg == "--device" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.Devices = append(svc.Devices, g.cfg.RunArgs[i])

		case strings.HasPrefix(arg, "--add-host="):
			svc.ExtraHosts = append(svc.ExtraHosts, strings.TrimPrefix(arg, "--add-host="))
		case arg == "--add-host" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.ExtraHosts = append(svc.ExtraHosts, g.cfg.RunArgs[i])

		// Network mode
		case strings.HasPrefix(arg, "--network="):
			svc.NetworkMode = strings.TrimPrefix(arg, "--network=")
		case strings.HasPrefix(arg, "--net="):
			svc.NetworkMode = strings.TrimPrefix(arg, "--net=")
		case arg == "--network" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.NetworkMode = g.cfg.RunArgs[i]
		case arg == "--net" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.NetworkMode = g.cfg.RunArgs[i]

		// IPC mode
		case strings.HasPrefix(arg, "--ipc="):
			svc.IpcMode = strings.TrimPrefix(arg, "--ipc=")
		case arg == "--ipc" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.IpcMode = g.cfg.RunArgs[i]

		// PID mode
		case strings.HasPrefix(arg, "--pid="):
			svc.PidMode = strings.TrimPrefix(arg, "--pid=")
		case arg == "--pid" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.PidMode = g.cfg.RunArgs[i]

		// Tmpfs
		case strings.HasPrefix(arg, "--tmpfs="):
			svc.Tmpfs = append(svc.Tmpfs, strings.TrimPrefix(arg, "--tmpfs="))
		case arg == "--tmpfs" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.Tmpfs = append(svc.Tmpfs, g.cfg.RunArgs[i])

		// Sysctl
		case strings.HasPrefix(arg, "--sysctl="):
			g.parseSysctl(svc, strings.TrimPrefix(arg, "--sysctl="))
		case arg == "--sysctl" && i+1 < len(g.cfg.RunArgs):
			i++
			g.parseSysctl(svc, g.cfg.RunArgs[i])

		// Publish ports
		case strings.HasPrefix(arg, "-p="):
			svc.Ports = append(svc.Ports, strings.TrimPrefix(arg, "-p="))
		case arg == "-p" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.Ports = append(svc.Ports, g.cfg.RunArgs[i])
		case strings.HasPrefix(arg, "--publish="):
			svc.Ports = append(svc.Ports, strings.TrimPrefix(arg, "--publish="))
		case arg == "--publish" && i+1 < len(g.cfg.RunArgs):
			i++
			svc.Ports = append(svc.Ports, g.cfg.RunArgs[i])
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

// parseSysctl parses a sysctl key=value pair and adds it to the service.
func (g *overrideGenerator) parseSysctl(svc *ServiceOverride, value string) {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) == 2 {
		if svc.Sysctls == nil {
			svc.Sysctls = make(map[string]string)
		}
		svc.Sysctls[parts[0]] = parts[1]
	}
}
