// Package features handles devcontainer feature resolution, caching, and installation.
package features

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Feature represents a resolved devcontainer feature.
type Feature struct {
	// ID is the original feature identifier from devcontainer.json
	ID string

	// Ref is the parsed reference (OCI, local, or HTTP)
	Ref FeatureRef

	// Options are the user-specified options for this feature
	Options map[string]interface{}

	// Metadata is the parsed devcontainer-feature.json
	Metadata *FeatureMetadata

	// CachePath is the local path to the cached feature contents
	CachePath string
}

// FeatureRef represents a parsed feature reference.
type FeatureRef struct {
	// Type is the reference type (oci, local, http)
	Type RefType

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

// RefType indicates the type of feature reference.
type RefType string

const (
	RefTypeOCI   RefType = "oci"
	RefTypeLocal RefType = "local"
	RefTypeHTTP  RefType = "http"
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

	// DependsOn lists required features (hard dependency)
	DependsOn []string `json:"dependsOn,omitempty"`

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
	Mounts []FeatureMount `json:"mounts,omitempty"`

	// OnCreateCommand runs after feature installation
	OnCreateCommand interface{} `json:"onCreateCommand,omitempty"`

	// UpdateContentCommand runs after onCreateCommand
	UpdateContentCommand interface{} `json:"updateContentCommand,omitempty"`

	// PostCreateCommand runs after all features installed
	PostCreateCommand interface{} `json:"postCreateCommand,omitempty"`

	// PostStartCommand runs on each container start
	PostStartCommand interface{} `json:"postStartCommand,omitempty"`

	// PostAttachCommand runs when attaching to the container
	PostAttachCommand interface{} `json:"postAttachCommand,omitempty"`

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

// FeatureMount represents a mount specification that can be either a string or an object.
type FeatureMount struct {
	Source string `json:"source,omitempty"`
	Target string `json:"target,omitempty"`
	Type   string `json:"type,omitempty"`
	// Raw holds the original string if mount was specified as a string
	Raw string `json:"-"`
}

// UnmarshalJSON handles both string and object forms of mount specifications.
func (m *FeatureMount) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		m.Raw = s
		// Parse the mount string: "source=...,target=...,type=..."
		parts := strings.Split(s, ",")
		for _, part := range parts {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			switch key {
			case "source", "src":
				m.Source = value
			case "target", "dst", "destination":
				m.Target = value
			case "type":
				m.Type = value
			}
		}
		return nil
	}

	// Try object form
	type mountAlias FeatureMount
	var obj mountAlias
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*m = FeatureMount(obj)
	return nil
}

// String returns the mount as a docker-style string.
func (m FeatureMount) String() string {
	if m.Raw != "" {
		return m.Raw
	}
	if m.Type == "" {
		m.Type = "bind"
	}
	return fmt.Sprintf("type=%s,source=%s,target=%s", m.Type, m.Source, m.Target)
}

// String returns the original feature ID.
func (f *Feature) String() string {
	return f.ID
}

// CanonicalID returns a canonical identifier for caching.
func (r *FeatureRef) CanonicalID() string {
	switch r.Type {
	case RefTypeOCI:
		return fmt.Sprintf("%s/%s/%s:%s", r.Registry, r.Repository, r.Resource, r.Version)
	case RefTypeLocal:
		return fmt.Sprintf("local:%s", r.Path)
	case RefTypeHTTP:
		return r.URL
	default:
		return ""
	}
}

// ParseFeatureRef parses a feature ID string into a FeatureRef.
func ParseFeatureRef(id string) (FeatureRef, error) {
	ref := FeatureRef{}

	// Check for local path
	if strings.HasPrefix(id, "./") || strings.HasPrefix(id, "../") || strings.HasPrefix(id, "/") {
		ref.Type = RefTypeLocal
		ref.Path = id
		return ref, nil
	}

	// Check for HTTP(S) URL
	if strings.HasPrefix(id, "http://") || strings.HasPrefix(id, "https://") {
		ref.Type = RefTypeHTTP
		ref.URL = id
		return ref, nil
	}

	// Parse as OCI reference
	// Format: [registry/]repository/feature[:version]
	ref.Type = RefTypeOCI

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

// GetOptionValue returns the effective value for an option.
func (f *Feature) GetOptionValue(name string) interface{} {
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

// GetEnvVars returns environment variables for the feature options.
func (f *Feature) GetEnvVars() map[string]string {
	env := make(map[string]string)

	if f.Metadata == nil {
		return env
	}

	// Add option values as environment variables
	for name := range f.Metadata.Options {
		val := f.GetOptionValue(name)
		if val != nil {
			// Normalize option name per devcontainer spec
			envName := NormalizeOptionName(name)
			strVal := fmt.Sprintf("%v", val)
			// Apply variable substitution
			env[envName] = substituteVariables(strVal)
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

// substituteVariables performs devcontainer variable substitution.
// Supports ${localEnv:VAR}, ${localEnv:VAR:default}, ${env:VAR}
func substituteVariables(s string) string {
	// Pattern: ${localEnv:VAR} or ${localEnv:VAR:default}
	localEnvPattern := regexp.MustCompile(`\$\{localEnv:([^}:]+)(?::([^}]*))?\}`)
	s = localEnvPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := localEnvPattern.FindStringSubmatch(match)
		if len(parts) >= 2 {
			value := os.Getenv(parts[1])
			if value == "" && len(parts) >= 3 {
				value = parts[2] // default value
			}
			return value
		}
		return match
	})

	// Pattern: ${env:VAR} (alias for localEnv)
	envPattern := regexp.MustCompile(`\$\{env:([^}:]+)(?::([^}]*))?\}`)
	s = envPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := envPattern.FindStringSubmatch(match)
		if len(parts) >= 2 {
			value := os.Getenv(parts[1])
			if value == "" && len(parts) >= 3 {
				value = parts[2]
			}
			return value
		}
		return match
	})

	return s
}
