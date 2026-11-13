// Package features handles devcontainer feature resolution, caching, and installation.
package features

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/griffithind/dcx/internal/lifecyclecmd"
	"github.com/griffithind/dcx/internal/mount"
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

	// Version is the version tag or digest
	Version string

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

	// Deprecated indicates this feature should not be used
	Deprecated bool `json:"deprecated,omitempty"`

	// Options defines available options
	Options map[string]OptionDefinition `json:"options,omitempty"`

	// InstallsAfter lists features this should install after (soft dependency)
	InstallsAfter []string `json:"installsAfter,omitempty"`

	// DependsOn specifies required features (hard dependency) with optional options
	// Map keys are feature IDs, values are feature options (can be empty object {})
	DependsOn map[string]interface{} `json:"dependsOn,omitempty"`

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

// CanonicalID returns a canonical identifier for caching.
func (r *FeatureSource) CanonicalID() string {
	switch r.Type {
	case SourceTypeOCI:
		return fmt.Sprintf("%s/%s/%s:%s", r.Registry, r.Repository, r.Resource, r.Version)
	case SourceTypeLocalPath:
		return fmt.Sprintf("local:%s", r.Path)
	case SourceTypeTarball:
		return r.URL
	default:
		return ""
	}
}

// ParseFeatureSource parses a feature ID string into a FeatureSource.
func ParseFeatureSource(id string) (FeatureSource, error) {
	ref := FeatureSource{}

	// Check for local path
	if strings.HasPrefix(id, "./") || strings.HasPrefix(id, "../") || strings.HasPrefix(id, "/") {
		ref.Type = SourceTypeLocalPath
		ref.Path = id
		return ref, nil
	}

	// Check for HTTP(S) URL
	if strings.HasPrefix(id, "http://") || strings.HasPrefix(id, "https://") {
		ref.Type = SourceTypeTarball
		ref.URL = id
		return ref, nil
	}

	// Parse as OCI reference
	// Format: [registry/]repository/feature[:version]
	ref.Type = SourceTypeOCI

	// Split off version
	versionIdx := strings.LastIndex(id, ":")
	if versionIdx != -1 && !strings.Contains(id[versionIdx:], "/") {
		ref.Version = id[versionIdx+1:]
		id = id[:versionIdx]
	} else {
		ref.Version = "latest"
	}

	// Parse registry/repository/feature
	parts := strings.Split(id, "/")
	if len(parts) < 2 {
		return ref, fmt.Errorf("invalid OCI feature reference: %s", id)
	}

	// Determine if first part is a registry
	if len(parts) >= 3 && (strings.Contains(parts[0], ".") || strings.Contains(parts[0], ":") || parts[0] == "localhost") {
		ref.Registry = parts[0]
		ref.Repository = strings.Join(parts[1:len(parts)-1], "/")
		ref.Resource = parts[len(parts)-1]
	} else {
		// Default registry is ghcr.io
		ref.Registry = "ghcr.io"
		ref.Repository = strings.Join(parts[:len(parts)-1], "/")
		ref.Resource = parts[len(parts)-1]
	}

	return ref, nil
}

// OptionValue returns the effective value for an option.
func (f *Feature) OptionValue(name string) interface{} {
	// Check user-specified options first
	if val, ok := f.Options[name]; ok {
		return val
	}

	// Fall back to default from metadata
	if f.Metadata != nil {
		if opt, ok := f.Metadata.Options[name]; ok {
			return opt.Default
		}
	}

	return nil
}

// EnvVars returns environment variables for the feature options.
func (f *Feature) EnvVars() map[string]string {
	env := make(map[string]string)

	if f.Metadata == nil {
		return env
	}

	// Add option values as environment variables
	for name := range f.Metadata.Options {
		val := f.OptionValue(name)
		if val != nil {
			// Normalize option name per devcontainer spec
			envName := NormalizeOptionName(name)
			// Options were substituted with the rest of devcontainer.json.
			env[envName] = fmt.Sprintf("%v", val)
		}
	}

	return env
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
