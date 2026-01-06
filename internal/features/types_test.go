package features

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFeatureRef_OCI(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected FeatureRef
	}{
		{
			name: "ghcr.io with version",
			id:   "ghcr.io/devcontainers/features/go:1",
			expected: FeatureRef{
				Type:       RefTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "go",
				Version:    "1",
			},
		},
		{
			name: "short form defaults to ghcr.io",
			id:   "devcontainers/features/node:18",
			expected: FeatureRef{
				Type:       RefTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "node",
				Version:    "18",
			},
		},
		{
			name: "no version defaults to latest",
			id:   "ghcr.io/devcontainers/features/python",
			expected: FeatureRef{
				Type:       RefTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "python",
				Version:    "latest",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseFeatureRef(tt.id)
			require.NoError(t, err)
			assert.Equal(t, tt.expected.Type, ref.Type)
			assert.Equal(t, tt.expected.Registry, ref.Registry)
			assert.Equal(t, tt.expected.Repository, ref.Repository)
			assert.Equal(t, tt.expected.Resource, ref.Resource)
			assert.Equal(t, tt.expected.Version, ref.Version)
		})
	}
}

func TestParseFeatureRef_Local(t *testing.T) {
	tests := []struct {
		name string
		id   string
		path string
	}{
		{
			name: "relative path with ./",
			id:   "./features/myfeature",
			path: "./features/myfeature",
		},
		{
			name: "relative path with ../",
			id:   "../shared/features/tool",
			path: "../shared/features/tool",
		},
		{
			name: "absolute path",
			id:   "/opt/features/custom",
			path: "/opt/features/custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseFeatureRef(tt.id)
			require.NoError(t, err)
			assert.Equal(t, RefTypeLocal, ref.Type)
			assert.Equal(t, tt.path, ref.Path)
		})
	}
}

func TestParseFeatureRef_HTTP(t *testing.T) {
	tests := []struct {
		name string
		id   string
		url  string
	}{
		{
			name: "https URL",
			id:   "https://example.com/features/tool.tgz",
			url:  "https://example.com/features/tool.tgz",
		},
		{
			name: "http URL",
			id:   "http://localhost:8080/feature.tar.gz",
			url:  "http://localhost:8080/feature.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseFeatureRef(tt.id)
			require.NoError(t, err)
			assert.Equal(t, RefTypeHTTP, ref.Type)
			assert.Equal(t, tt.url, ref.URL)
		})
	}
}

func TestFeature_GetEnvVars(t *testing.T) {
	feature := &Feature{
		ID: "test-feature",
		Options: map[string]interface{}{
			"version": "1.19",
			"modules": "on",
		},
		Metadata: &FeatureMetadata{
			Options: map[string]OptionDefinition{
				"version": {
					Type:    "string",
					Default: "latest",
				},
				"modules": {
					Type:    "string",
					Default: "off",
				},
				"unused": {
					Type:    "string",
					Default: "default-value",
				},
			},
		},
	}

	env := feature.GetEnvVars()

	// Should have user-specified values
	assert.Equal(t, "1.19", env["VERSION"])
	assert.Equal(t, "on", env["MODULES"])

	// Should have default for unspecified option
	assert.Equal(t, "default-value", env["UNUSED"])
}

func TestFeatureRef_CanonicalID(t *testing.T) {
	tests := []struct {
		name     string
		ref      FeatureRef
		expected string
	}{
		{
			name: "OCI reference",
			ref: FeatureRef{
				Type:       RefTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "go",
				Version:    "1.19",
			},
			expected: "ghcr.io/devcontainers/features/go:1.19",
		},
		{
			name: "local reference",
			ref: FeatureRef{
				Type: RefTypeLocal,
				Path: "./features/custom",
			},
			expected: "local:./features/custom",
		},
		{
			name: "HTTP reference",
			ref: FeatureRef{
				Type: RefTypeHTTP,
				URL:  "https://example.com/feature.tgz",
			},
			expected: "https://example.com/feature.tgz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.ref.CanonicalID())
		})
	}
}

// TestOuzoERPStyleFeatures tests parsing of features used in ouzoerp-style configurations.
func TestOuzoERPStyleFeatures(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected FeatureRef
	}{
		{
			name: "common-utils with major version",
			id:   "ghcr.io/devcontainers/features/common-utils:2",
			expected: FeatureRef{
				Type:       RefTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "common-utils",
				Version:    "2",
			},
		},
		{
			name: "docker-outside-of-docker",
			id:   "ghcr.io/devcontainers/features/docker-outside-of-docker:1",
			expected: FeatureRef{
				Type:       RefTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "docker-outside-of-docker",
				Version:    "1",
			},
		},
		{
			name: "feature with full semver",
			id:   "ghcr.io/devcontainers/features/node:1.2.3",
			expected: FeatureRef{
				Type:       RefTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "node",
				Version:    "1.2.3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseFeatureRef(tt.id)
			require.NoError(t, err)
			assert.Equal(t, tt.expected.Type, ref.Type)
			assert.Equal(t, tt.expected.Registry, ref.Registry)
			assert.Equal(t, tt.expected.Repository, ref.Repository)
			assert.Equal(t, tt.expected.Resource, ref.Resource)
			assert.Equal(t, tt.expected.Version, ref.Version)
		})
	}
}

// TestFeatureOptionsHandling tests that feature options are correctly handled.
func TestFeatureOptionsHandling(t *testing.T) {
	// Test common-utils style options
	feature := &Feature{
		ID: "ghcr.io/devcontainers/features/common-utils:2",
		Options: map[string]interface{}{
			"username":              "testuser",
			"installZsh":            false,
			"installOhMyZsh":        false,
			"installOhMyZshConfig":  false,
			"upgradePackages":       false,
		},
		Metadata: &FeatureMetadata{
			Options: map[string]OptionDefinition{
				"username": {
					Type:    "string",
					Default: "automatic",
				},
				"installZsh": {
					Type:    "boolean",
					Default: true,
				},
				"installOhMyZsh": {
					Type:    "boolean",
					Default: true,
				},
				"installOhMyZshConfig": {
					Type:    "boolean",
					Default: true,
				},
				"upgradePackages": {
					Type:    "boolean",
					Default: true,
				},
			},
		},
	}

	env := feature.GetEnvVars()

	// User-specified values should be set
	assert.Equal(t, "testuser", env["USERNAME"])
	assert.Equal(t, "false", env["INSTALLZSH"])
	assert.Equal(t, "false", env["INSTALLOHMYZSH"])
	assert.Equal(t, "false", env["INSTALLOHMYZSHCONFIG"])
	assert.Equal(t, "false", env["UPGRADEPACKAGES"])
}

// TestFeatureOptionsWithBooleans tests boolean option handling.
func TestFeatureOptionsWithBooleans(t *testing.T) {
	tests := []struct {
		name     string
		options  map[string]interface{}
		optDef   map[string]OptionDefinition
		expected map[string]string
	}{
		{
			name: "boolean false",
			options: map[string]interface{}{
				"enabled": false,
			},
			optDef: map[string]OptionDefinition{
				"enabled": {Type: "boolean", Default: true},
			},
			expected: map[string]string{
				"ENABLED": "false",
			},
		},
		{
			name: "boolean true",
			options: map[string]interface{}{
				"enabled": true,
			},
			optDef: map[string]OptionDefinition{
				"enabled": {Type: "boolean", Default: false},
			},
			expected: map[string]string{
				"ENABLED": "true",
			},
		},
		{
			name: "docker-outside-of-docker moby option",
			options: map[string]interface{}{
				"moby": false,
			},
			optDef: map[string]OptionDefinition{
				"moby": {Type: "boolean", Default: true},
			},
			expected: map[string]string{
				"MOBY": "false",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			feature := &Feature{
				ID:      "test-feature",
				Options: tt.options,
				Metadata: &FeatureMetadata{
					Options: tt.optDef,
				},
			}

			env := feature.GetEnvVars()
			for k, v := range tt.expected {
				assert.Equal(t, v, env[k])
			}
		})
	}
}
