// Package features handles devcontainer feature resolution, caching, and installation.
package features

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/griffithind/dcx/internal/lifecyclecmd"
	"github.com/griffithind/dcx/internal/mount"
	"github.com/griffithind/dcx/internal/shell"
)

// Feature represents a resolved devcontainer feature.
type Feature struct {
	// ID is the original feature identifier from devcontainer.json
	ID string

	// Source is the parsed feature source (OCI, local path, or tarball)
	Source FeatureSource

	// Options are the user-specified options for this feature
	Options map[string]interface{}

	// Metadata is the parsed devcontainer-feature.json
	Metadata *FeatureMetadata

	// CachePath is the local path to the cached feature contents
	CachePath string

	// ManifestDigest is the OCI manifest digest (sha256:...).
	// Only populated for OCI features.
	ManifestDigest string

	// Integrity is the SHA256 checksum of the feature tarball (sha256:...).
	// Used for lockfile integrity verification.
	Integrity string
}

// FeatureSource represents a parsed feature source/reference.
type FeatureSource struct {
	// Type is the source type (oci, local-path, tarball)
	Type SourceType

	// Registry is the OCI registry (for OCI refs)
	Registry string

	// Repository is the repository path (for OCI refs)
	Repository string

	// Resource is the feature name within the repository
	Resource string

	// Version is the most specific version: the digest, else the tag.
	Version string

	// Tag is the tag the reference names ("latest" when it names neither
	// a tag nor a digest; "" when it names only a digest).
	Tag string

	// Path is the local path (for local refs)
	Path string

	// URL is the HTTP URL (for HTTP refs)
	URL string
}

// SourceType indicates the type of feature reference.
type SourceType string

const (
	SourceTypeOCI       SourceType = "oci"
	SourceTypeLocalPath SourceType = "local"
	SourceTypeTarball   SourceType = "http"
)

// FeatureMetadata represents the devcontainer-feature.json contents.
type FeatureMetadata struct {
	// ID is the feature identifier
	ID string `json:"id"`

	// Version is the feature version
	Version string `json:"version"`

	// Name is the display name
	Name string `json:"name"`

	// Description describes the feature
	Description string `json:"description,omitempty"`

	// DocumentationURL links to documentation
	DocumentationURL string `json:"documentationURL,omitempty"`

	// LicenseURL links to the license
	LicenseURL string `json:"licenseURL,omitempty"`

	// Keywords for searchability
	Keywords []string `json:"keywords,omitempty"`

	// LegacyIds lists old identifiers that should resolve to this feature
	LegacyIds []string `json:"legacyIds,omitempty"`

	// CurrentID is the feature's current id when this package is published
	// under an old one.
	CurrentID string `json:"currentId,omitempty"`

	// Deprecated indicates this feature should not be used
	Deprecated bool `json:"deprecated,omitempty"`

	// Options defines available options
	Options map[string]OptionDefinition `json:"options,omitempty"`

	// InstallsAfter lists features this should install after (soft dependency)
	InstallsAfter []string `json:"installsAfter,omitempty"`

	// DependsOn lists required features (hard dependencies) with their
	// options, in the order the feature declares them.
	DependsOn Dependencies `json:"dependsOn,omitempty"`

	// ContainerEnv specifies environment variables to set
	ContainerEnv map[string]string `json:"containerEnv,omitempty"`

	// CapAdd specifies capabilities to add
	CapAdd []string `json:"capAdd,omitempty"`

	// SecurityOpt specifies security options
	SecurityOpt []string `json:"securityOpt,omitempty"`

	// Privileged indicates if privileged mode is needed
	Privileged bool `json:"privileged,omitempty"`

	// Init indicates if init process is needed
	Init bool `json:"init,omitempty"`

	// Entrypoint specifies a custom entrypoint
	Entrypoint string `json:"entrypoint,omitempty"`

	// Mounts specifies additional mounts (can be strings or objects)
	Mounts []mount.Mount `json:"mounts,omitempty"`

	// OnCreateCommand runs after feature installation
	OnCreateCommand *lifecyclecmd.Command `json:"onCreateCommand,omitempty"`

	// UpdateContentCommand runs after onCreateCommand
	UpdateContentCommand *lifecyclecmd.Command `json:"updateContentCommand,omitempty"`

	// PostCreateCommand runs after all features installed
	PostCreateCommand *lifecyclecmd.Command `json:"postCreateCommand,omitempty"`

	// PostStartCommand runs on each container start
	PostStartCommand *lifecyclecmd.Command `json:"postStartCommand,omitempty"`

	// PostAttachCommand runs when attaching to the container
	PostAttachCommand *lifecyclecmd.Command `json:"postAttachCommand,omitempty"`

	// Customizations contains tool-specific configuration
	Customizations map[string]interface{} `json:"customizations,omitempty"`
}

// Dependencies is a feature's dependsOn object, in declaration order (the
// lockfile records them in that order).
type Dependencies []Dependency

// Dependency is one dependsOn entry: a feature reference and the options
// to install it with.
type Dependency struct {
	ID      string
	Options map[string]interface{}
}

// UnmarshalJSON reads a dependsOn object, keeping its key order.
func (d *Dependencies) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		*d = nil
		return nil
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("dependsOn must be an object")
	}
	var deps Dependencies
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return err
		}
		var value interface{}
		if err := dec.Decode(&value); err != nil {
			return err
		}
		deps = append(deps, Dependency{ID: key.(string), Options: featureOptions(value)})
	}
	*d = deps
	return nil
}

// OptionDefinition defines a feature option.
type OptionDefinition struct {
	// Type is the option type (boolean, string, enum)
	Type string `json:"type"`

	// Default is the default value
	Default interface{} `json:"default,omitempty"`

	// Description describes the option
	Description string `json:"description,omitempty"`

	// Enum lists valid values for enum types
	Enum []string `json:"enum,omitempty"`

	// Proposals suggests values for string types
	Proposals []string `json:"proposals,omitempty"`
}

// String returns the original feature ID.
func (f *Feature) String() string {
	return f.ID
}

// IsDigest reports whether the reference pins a manifest digest
// ("…@sha256:<hex>") rather than naming a tag, which can move.
func (r *FeatureSource) IsDigest() bool {
	return strings.HasPrefix(r.Version, "sha256:")
}

// CanonicalID returns a canonical identifier for caching.
func (r *FeatureSource) CanonicalID() string {
	switch r.Type {
	case SourceTypeOCI:
		if r.IsDigest() {
			return fmt.Sprintf("%s/%s/%s@%s", r.Registry, r.Repository, r.Resource, r.Version)
		}
		return fmt.Sprintf("%s/%s/%s:%s", r.Registry, r.Repository, r.Resource, r.Version)
	case SourceTypeLocalPath:
		return fmt.Sprintf("local:%s", r.Path)
	case SourceTypeTarball:
		return r.URL
	default:
		return ""
	}
}

// scheme is how the registry is reached: plain http for localhost (a local
// test registry), https otherwise, as devcontainers/cli does.
func (r *FeatureSource) scheme() string {
	host := r.Registry
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "localhost" {
		return "http"
	}
	return "https"
}

// Reference syntax, as devcontainers/cli validates it.
var (
	ociPath         = regexp.MustCompile(`^[a-z0-9]+([._-][a-z0-9]+)*(/[a-z0-9]+([._-][a-z0-9]+)*)*$`)
	ociTag          = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	featureIDSyntax = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	tarballName     = regexp.MustCompile(`devcontainer-feature-(.*)\.tgz`)
)

// ParseFeatureSource parses a feature reference the way devcontainers/cli
// does:
//   - "https://…/devcontainer-feature-<id>.tgz" is a tarball;
//   - "./x" and "../x" are local features, relative to the config file;
//   - "<registry>/<namespace>/<id>[:tag][@sha256:…]" is an OCI feature.
//     References are case-insensitive, the registry must be a host name
//     (with a dot, or localhost), and "devcontainers-contrib" is the old
//     name of "devcontainers-extra".
//
// Legacy short ids ("node", "golang") map to ghcr.io/devcontainers/features
// (LegacyFeatureRef). Absolute paths, plain-http tarballs and the
// GitHub-release form ("owner/repo/feature") are rejected.
func ParseFeatureSource(id string) (FeatureSource, error) {
	if mapped, ok := LegacyFeatureRef(id); ok {
		id = mapped
	}
	switch {
	case !strings.ContainsAny(id, "/\\"):
		return FeatureSource{}, fmt.Errorf("legacy feature %q is not supported: see https://containers.dev/features for its replacement, or prefix a local feature with \"./\"", id)
	case strings.HasPrefix(id, "https://"):
		u, err := url.Parse(id)
		if err != nil {
			return FeatureSource{}, fmt.Errorf("invalid feature URL %q: %w", id, err)
		}
		m := tarballName.FindStringSubmatch(path.Base(u.Path))
		if m == nil || !featureIDSyntax.MatchString(m[1]) {
			return FeatureSource{}, fmt.Errorf("feature tarball %q must be named devcontainer-feature-<id>.tgz", id)
		}
		return FeatureSource{Type: SourceTypeTarball, URL: id}, nil
	case strings.HasPrefix(id, "http://"):
		return FeatureSource{}, fmt.Errorf("feature tarball %q must be fetched over https", id)
	case strings.HasPrefix(id, "./") || strings.HasPrefix(id, "../"):
		return FeatureSource{Type: SourceTypeLocalPath, Path: id}, nil
	case strings.HasPrefix(id, "/"):
		return FeatureSource{}, fmt.Errorf("local feature %q: an absolute path is not allowed; use a path relative to the config file", id)
	}
	return parseOCIRef(id)
}

// parseOCIRef parses "<registry>/<namespace>/<id>[:tag][@sha256:<hex>]"
// (devcontainers/cli's getRef).
func parseOCIRef(id string) (FeatureSource, error) {
	input := strings.ToLower(id)
	if strings.HasPrefix(input, ".") {
		return FeatureSource{}, fmt.Errorf("invalid feature reference %q", id)
	}
	ref := FeatureSource{Type: SourceTypeOCI}
	resource := input
	lastSlash := strings.LastIndex(input, "/")
	if at := strings.LastIndex(input, "@"); at != -1 {
		resource = input[:at]
		if c := strings.LastIndex(resource, ":"); c > strings.LastIndex(resource, "/") {
			ref.Tag = resource[c+1:]
			resource = resource[:c]
		}
		digest := input[at+1:]
		algo, hexPart, ok := strings.Cut(digest, ":")
		if !ok || algo != "sha256" || len(hexPart) != 64 || strings.Trim(hexPart, "0123456789abcdef") != "" {
			return FeatureSource{}, fmt.Errorf("invalid feature reference %q: the digest must be sha256:<64 hex digits>", id)
		}
		ref.Version = digest
	} else if c := strings.LastIndex(input, ":"); c != -1 && c > lastSlash {
		resource = input[:c]
		ref.Tag = input[c+1:]
		ref.Version = ref.Tag
	} else {
		ref.Tag = "latest"
		ref.Version = ref.Tag
	}
	if ref.Tag != "" && !ociTag.MatchString(ref.Tag) {
		return FeatureSource{}, fmt.Errorf("invalid feature reference %q: bad tag %q", id, ref.Tag)
	}

	parts := strings.Split(resource, "/")
	if len(parts) > 1 && parts[1] == "devcontainers-contrib" {
		parts[1] = "devcontainers-extra"
	}
	ref.Registry = parts[0]
	ref.Repository = strings.Join(parts[1:len(parts)-1], "/")
	ref.Resource = parts[len(parts)-1]
	if !ociPath.MatchString(ref.Repository + "/" + ref.Resource) {
		return FeatureSource{}, fmt.Errorf("invalid feature reference %q: expected <registry>/<namespace>/<feature>", id)
	}
	if !strings.Contains(ref.Registry, ".") && !strings.HasPrefix(ref.Registry, "localhost") {
		return FeatureSource{}, fmt.Errorf("feature %q has no registry: the GitHub-release form is not supported; use the OCI reference (e.g. ghcr.io/%s)", id, resource)
	}
	return ref, nil
}

// legacyFeatures are the short ids of the features that moved from
// microsoft/vscode-dev-containers to ghcr.io/devcontainers/features, with
// their new names.
var legacyFeatures = map[string]string{
	"aws-cli": "aws-cli", "azure-cli": "azure-cli", "desktop-lite": "desktop-lite",
	"docker-in-docker": "docker-in-docker", "docker-from-docker": "docker-from-docker",
	"dotnet": "dotnet", "git": "git", "git-lfs": "git-lfs", "github-cli": "github-cli",
	"java": "java", "kubectl-helm-minikube": "kubectl-helm-minikube", "node": "node",
	"powershell": "powershell", "python": "python", "ruby": "ruby", "rust": "rust",
	"sshd": "sshd", "terraform": "terraform",
	"golang": "go", "common": "common-utils",
}

// LegacyFeatureRef maps a legacy short feature id ("node", "golang") to
// its ghcr.io/devcontainers/features reference, pinned to major version 1
// as devcontainers/cli does. ok is false for any other id.
func LegacyFeatureRef(id string) (string, bool) {
	name, ok := legacyFeatures[id]
	if !ok {
		return "", false
	}
	return "ghcr.io/devcontainers/features/" + name + ":1", true
}

// featureOptions turns a devcontainer.json (or dependsOn) feature value
// into options: an object is the options, a string is the "version"
// option, and true or false installs the feature with its defaults (as
// devcontainers/cli does for both).
func featureOptions(value interface{}) map[string]interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return v
	case string:
		return map[string]interface{}{"version": v}
	default:
		return map[string]interface{}{}
	}
}

// EnvFile renders the feature's devcontainer-features.env, which its
// install step sources: every option with a default, plus every option the
// user set (declared or not), as devcontainers/cli writes it. The values
// are shell-quoted; one feature's options never reach another's install.
func (f *Feature) EnvFile() string {
	values := map[string]string{}
	if f.Metadata != nil {
		for name, opt := range f.Metadata.Options {
			if opt.Default != nil {
				values[NormalizeOptionName(name)] = fmt.Sprint(opt.Default)
			}
		}
	}
	for name, val := range f.Options {
		if val != nil {
			values[NormalizeOptionName(name)] = fmt.Sprint(val)
		}
	}
	var sb strings.Builder
	for _, key := range sortedKeys(values) {
		fmt.Fprintf(&sb, "%s=%s\n", key, shell.Quote(values[key]))
	}
	return sb.String()
}

// optionNameNonWord matches any character that is not alphanumeric or underscore
var optionNameNonWord = regexp.MustCompile(`[^\w_]`)

// optionNameLeadingInvalid matches leading digits and underscores
var optionNameLeadingInvalid = regexp.MustCompile(`^[\d_]+`)

// NormalizeOptionName transforms an option name to a valid environment variable name
// per the devcontainer features specification:
// str.replace(/[^\w_]/g, '_').replace(/^[\d_]+/g, '_').toUpperCase()
func NormalizeOptionName(name string) string {
	// Replace non-word characters with underscores
	name = optionNameNonWord.ReplaceAllString(name, "_")
	// Replace leading digits and underscores with a single underscore
	name = optionNameLeadingInvalid.ReplaceAllString(name, "_")
	// Convert to uppercase
	return strings.ToUpper(name)
}
