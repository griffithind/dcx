package devcontainer

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
)

// ComposeConfigSource returns the compose model of a project: the output
// of `docker compose config --format json` with every profile. The service
// implements it with the container engine; resolution never runs docker
// itself.
type ComposeConfigSource interface {
	ComposeConfig(ctx context.Context, project, dir string, files []string) ([]byte, error)
}

// ComposeConfig is the compose model dcx reads: the project as docker
// compose resolves it (overrides merged, includes and extends followed,
// variables interpolated, relative paths resolved against the project
// directory). Metadata discovery, the staleness key and provisioning all
// read it, so they agree on which service and Dockerfile they mean.
type ComposeConfig struct {
	Name     string                     `json:"name"`
	Services map[string]ComposeService  `json:"services"`
	Volumes  map[string]json.RawMessage `json:"volumes"`
}

// ComposeService is one service of the compose model.
type ComposeService struct {
	Image       string          `json:"image"`
	Build       *ComposeBuild   `json:"build"`
	NetworkMode string          `json:"network_mode"`
	Entrypoint  json.RawMessage `json:"entrypoint"`
	Command     json.RawMessage `json:"command"`
	Ports       []ComposePort   `json:"ports"`
	User        string          `json:"user"`
}

// ComposeBuild is a service's build section. Compose writes the context as
// an absolute path (or a URL) and the Dockerfile relative to it.
type ComposeBuild struct {
	Context          string            `json:"context"`
	Dockerfile       string            `json:"dockerfile"`
	DockerfileInline string            `json:"dockerfile_inline"`
	Target           string            `json:"target"`
	Args             map[string]string `json:"args"`
}

// ComposePort is a port a service publishes.
type ComposePort struct {
	Target    int    `json:"target"`
	Published string `json:"published"`
	HostIP    string `json:"host_ip"`
	Protocol  string `json:"protocol"`
}

// ParseComposeConfig parses `docker compose config --format json` output.
func ParseComposeConfig(data []byte) (*ComposeConfig, error) {
	var c ComposeConfig
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse compose config: %w", err)
	}
	return &c, nil
}

// loadComposeConfig reads the compose model of the config's project.
func loadComposeConfig(ctx context.Context, src ComposeConfigSource, project, configPath string, files []string) (*ComposeConfig, error) {
	out, err := src.ComposeConfig(ctx, project, filepath.Dir(configPath), files)
	if err != nil {
		return nil, fmt.Errorf("failed to get compose config: %w", err)
	}
	return ParseComposeConfig(out)
}

// DockerfilePath returns the Dockerfile a build reads, "" when it is not a
// local file (an inline Dockerfile, a Git or URL context).
func (b *ComposeBuild) DockerfilePath() string {
	if b == nil || b.DockerfileInline != "" {
		return ""
	}
	dockerfile := b.Dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if filepath.IsAbs(dockerfile) {
		return dockerfile
	}
	if !filepath.IsAbs(b.Context) {
		return ""
	}
	return filepath.Join(b.Context, dockerfile)
}

// Dockerfiles returns the local Dockerfiles of every service's build.
func (c *ComposeConfig) Dockerfiles() []string {
	if c == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, svc := range c.Services {
		if p := svc.Build.DockerfilePath(); p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// baseImage returns the image whose devcontainer.metadata applies to the
// service: the base image of its build, else its image.
func (c *ComposeConfig) baseImage(service string) string {
	if c == nil {
		return ""
	}
	svc, ok := c.Services[service]
	if !ok {
		return ""
	}
	if svc.Build != nil {
		return dockerfileBaseImage(svc.Build.DockerfilePath(), svc.Build.Target, svc.Build.Args)
	}
	return svc.Image
}

// Forward converts a published port (false unless it is published on a
// fixed host port: not for a Docker-assigned port or a range).
func (p ComposePort) Forward() (PortForward, bool) {
	n, err := strconv.Atoi(p.Published)
	if err != nil || p.Target <= 0 {
		return PortForward{}, false
	}
	return PortForward{ContainerPort: p.Target, HostPort: n, Host: p.HostIP, Protocol: p.Protocol}, true
}
