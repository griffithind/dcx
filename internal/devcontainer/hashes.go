package devcontainer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/features"
	"gopkg.in/yaml.v3"
)

// ComputeConfigHash computes a single hash of all build inputs for a devcontainer:
// devcontainer.json content, Dockerfile content, compose files (including referenced
// Dockerfiles from service build directives), and feature configuration.
//
// This hash is used for both staleness detection and image cache tagging.
// Any change to any input produces a different hash.
func ComputeConfigHash(cfg *DevContainerConfig, dockerfilePath string, composeFiles []string, resolvedFeatures []*features.Feature) (string, error) {
	h := sha256.New()

	// 1. devcontainer.json content
	if raw := cfg.GetRawJSON(); len(raw) > 0 {
		h.Write(raw)
	} else {
		data, err := json.Marshal(cfg)
		if err != nil {
			return "", fmt.Errorf("marshal config for hash: %w", err)
		}
		h.Write(data)
	}

	// 2. Dockerfile content (for DockerfilePlan)
	if dockerfilePath != "" {
		if content, err := os.ReadFile(dockerfilePath); err == nil {
			h.Write([]byte("\x00dockerfile\x00"))
			h.Write(content)
		}
	}

	// 3. Compose files and their referenced Dockerfiles
	if len(composeFiles) > 0 {
		for _, f := range composeFiles {
			content, err := os.ReadFile(f)
			if err != nil {
				return "", fmt.Errorf("read compose file %s: %w", f, err)
			}
			h.Write([]byte("\x00compose:" + f + "\x00"))
			h.Write(content)
		}

		// Include Dockerfiles referenced by compose service build directives.
		// Sort paths for deterministic hashing.
		dockerfilePaths := collectComposeDockerfiles(composeFiles)
		sort.Strings(dockerfilePaths)

		for _, df := range dockerfilePaths {
			content, err := os.ReadFile(df)
			if err != nil {
				// Skip Dockerfiles that don't exist (may be generated later)
				continue
			}
			h.Write([]byte("\x00compose-dockerfile:" + df + "\x00"))
			h.Write(content)
		}
	}

	// 4. Features configuration
	if len(resolvedFeatures) > 0 {
		var featureData []string
		for _, f := range resolvedFeatures {
			optData, _ := json.Marshal(f.Options)
			version := ""
			if f.Metadata != nil {
				version = f.Metadata.Version
			}
			featureData = append(featureData, fmt.Sprintf("%s:%s:%s", f.ID, version, string(optData)))
		}
		sort.Strings(featureData)
		h.Write([]byte("\x00features\x00"))
		h.Write([]byte(strings.Join(featureData, "|")))
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// collectComposeDockerfiles parses compose files and returns absolute paths
// to all Dockerfiles referenced by service build directives.
func collectComposeDockerfiles(composeFiles []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, f := range composeFiles {
		content, err := os.ReadFile(f)
		if err != nil {
			continue
		}

		paths := parseComposeDockerfilePaths(content, filepath.Dir(f))
		for _, p := range paths {
			if !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		}
	}

	return result
}

// composeFile is a minimal representation of a docker-compose.yml for
// extracting build Dockerfile references.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

// composeService represents a single service in a compose file.
type composeService struct {
	Build composeBuild `yaml:"build"`
}

// composeBuild handles both string and object forms of the build directive.
// String form: build: ./path (context only, Dockerfile defaults to "Dockerfile")
// Object form: build: { context: ./path, dockerfile: Dockerfile.dev }
type composeBuild struct {
	Context    string `yaml:"context"`
	Dockerfile string `yaml:"dockerfile"`
}

// UnmarshalYAML handles both string and object forms of the build directive.
func (b *composeBuild) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		// String form: build: ./path
		b.Context = value.Value
		return nil
	}
	// Object form - decode fields
	type plain composeBuild
	return value.Decode((*plain)(b))
}

// parseComposeDockerfilePaths extracts Dockerfile paths from compose YAML content.
// baseDir is the directory of the compose file, used to resolve relative paths.
func parseComposeDockerfilePaths(content []byte, baseDir string) []string {
	var cf composeFile
	if err := yaml.Unmarshal(content, &cf); err != nil {
		return nil
	}

	var paths []string
	for _, svc := range cf.Services {
		if svc.Build.Context == "" && svc.Build.Dockerfile == "" {
			// No build directive (image-only service)
			continue
		}

		context := svc.Build.Context
		if context == "" {
			context = "."
		}

		// Resolve context relative to compose file directory
		if !filepath.IsAbs(context) {
			context = filepath.Join(baseDir, context)
		}

		dockerfile := svc.Build.Dockerfile
		if dockerfile == "" {
			dockerfile = "Dockerfile"
		}

		// Resolve dockerfile relative to context directory
		var absPath string
		if filepath.IsAbs(dockerfile) {
			absPath = dockerfile
		} else {
			absPath = filepath.Join(context, dockerfile)
		}

		paths = append(paths, absPath)
	}

	return paths
}
