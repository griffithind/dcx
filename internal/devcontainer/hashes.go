package devcontainer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/features"
	"gopkg.in/yaml.v3"
)

// ComputeConfigHash computes the staleness key of a devcontainer: when it
// changes, `dcx up` recreates the container. It covers only what the user
// edits:
//   - devcontainer.json as written (before variable substitution, so a
//     ${localEnv:…} value that differs between shells or logins is not a
//     change), normalized (comments, formatting and key order don't matter)
//     and without properties that don't affect the container: name,
//     customizations, ports attributes, remoteEnv, host requirements,
//     waitFor, userEnvProbe, shutdownAction, and the lifecycle commands that
//     run on every start/attach (initializeCommand, postStartCommand,
//     postAttachCommand);
//   - the Dockerfile, compose files and Dockerfiles referenced by compose
//     services;
//   - the files of local features.
//
// What else determines the container (substituted values, the digests a
// feature tag resolves to) is ComputeInputsHash: when only that changes,
// dcx reports the drift instead of recreating.
func ComputeConfigHash(raw *DevContainerConfig, dockerfilePath string, composeFiles []string, resolvedFeatures []*features.Feature) (string, error) {
	return computeHash(raw, dockerfilePath, composeFiles, localFeaturesKey(resolvedFeatures))
}

// ComputeInputsHash covers everything that determines the container: the
// substituted local config (before image-metadata merging, so it does not
// depend on whether the base image happens to be available) and the
// resolved features with their versions and digests (FeaturesKey).
func ComputeInputsHash(local *DevContainerConfig, dockerfilePath string, composeFiles []string, resolvedFeatures []*features.Feature) (string, error) {
	return computeHash(local, dockerfilePath, composeFiles, FeaturesKey(resolvedFeatures))
}

func computeHash(cfg *DevContainerConfig, dockerfilePath string, composeFiles []string, featuresKey string) (string, error) {
	h := sha256.New()

	norm, err := normalizedConfigJSON(cfg)
	if err != nil {
		return "", err
	}
	h.Write([]byte("config\x00"))
	h.Write(norm)

	// Dockerfile content (for DockerfilePlan)
	if dockerfilePath != "" {
		if content, err := os.ReadFile(dockerfilePath); err == nil {
			h.Write([]byte("\x00dockerfile\x00"))
			h.Write(content)
		}
	}

	// Compose files and their referenced Dockerfiles
	if len(composeFiles) > 0 {
		for _, f := range composeFiles {
			content, err := os.ReadFile(f)
			if err != nil {
				return "", fmt.Errorf("read compose file %s: %w", f, err)
			}
			h.Write([]byte("\x00compose:" + filepath.Base(f) + "\x00"))
			h.Write(content)
		}

		dockerfilePaths := collectComposeDockerfiles(composeFiles)
		sort.Strings(dockerfilePaths)
		for _, df := range dockerfilePaths {
			content, err := os.ReadFile(df)
			if err != nil {
				continue
			}
			h.Write([]byte("\x00compose-dockerfile\x00"))
			h.Write(content)
		}
	}

	if featuresKey != "" {
		h.Write([]byte("\x00features\x00"))
		h.Write([]byte(featuresKey))
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// localFeaturesKey identifies the files of the local (./path) features;
// "" without any.
func localFeaturesKey(feats []*features.Feature) string {
	var entries []string
	for _, f := range feats {
		if f.Source.Type == features.SourceTypeLocalPath && f.CachePath != "" {
			entries = append(entries, f.ID+"\x1f"+hashDir(f.CachePath))
		}
	}
	if len(entries) == 0 {
		return ""
	}
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\x1e")))
	return hex.EncodeToString(sum[:])
}

// normalizedConfigJSON marshals the container-relevant part of cfg.
// encoding/json sorts map keys, so the result is deterministic.
func normalizedConfigJSON(cfg *DevContainerConfig) ([]byte, error) {
	if cfg == nil {
		return []byte("null"), nil
	}
	n := *cfg
	n.Name = ""
	n.Customizations = nil
	n.PortsAttributes = nil
	n.OtherPortsAttributes = nil
	n.RemoteEnv = nil
	n.HostRequirements = nil
	n.WaitFor = ""
	n.UserEnvProbe = ""
	n.ShutdownAction = ""
	n.InitializeCommand = nil
	n.PostStartCommand = nil
	n.PostAttachCommand = nil
	data, err := json.Marshal(&n)
	if err != nil {
		return nil, fmt.Errorf("marshal config for hash: %w", err)
	}
	return data, nil
}

// FeaturesKey identifies a resolved feature set: feature IDs, versions,
// options, registry digests and, for local features, a hash of their files,
// so editing a local feature's install.sh changes the key. It is part of
// hash.inputs and ignores the install order, so it must not change (that
// would report drift for every existing container); the features image
// is keyed with FeaturesLayerKey.
func FeaturesKey(feats []*features.Feature) string {
	entries := featureEntries(feats)
	sort.Strings(entries)
	sum := sha256.Sum256([]byte(strings.Join(entries, "\x1e")))
	return hex.EncodeToString(sum[:])
}

// FeaturesLayerKey identifies the features image's inputs: what FeaturesKey
// covers, in install order, since the same features installed in another
// order make another image.
func FeaturesLayerKey(feats []*features.Feature) string {
	sum := sha256.Sum256([]byte(strings.Join(featureEntries(feats), "\x1e")))
	return hex.EncodeToString(sum[:])
}

// featureEntries describes each feature, in install order.
func featureEntries(feats []*features.Feature) []string {
	entries := make([]string, 0, len(feats))
	for _, f := range feats {
		opts, _ := json.Marshal(f.Options)
		version := ""
		if f.Metadata != nil {
			version = f.Metadata.Version
		}
		content := ""
		if f.Source.Type == features.SourceTypeLocalPath && f.CachePath != "" {
			content = hashDir(f.CachePath)
		}
		entries = append(entries, strings.Join([]string{f.ID, version, string(opts), f.ManifestDigest, f.Integrity, content}, "\x1f"))
	}
	return entries
}

// hashDir hashes the relative paths, modes and contents of all regular
// files under dir. Unreadable entries are skipped.
func hashDir(dir string) string {
	h := sha256.New()
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		_, _ = fmt.Fprintf(h, "%s\x00%o\x00%d\x00", filepath.ToSlash(rel), info.Mode().Perm(), len(data)) // hash.Hash writes never fail
		h.Write(data)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
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
