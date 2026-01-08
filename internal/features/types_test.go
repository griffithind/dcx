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

// TestFeatureMetadataFields tests parsing of all feature metadata fields.
func TestFeatureMetadataFields(t *testing.T) {
	metadata := &FeatureMetadata{
		ID:               "my-feature",
		Version:          "1.2.3",
		Name:             "My Feature",
		Description:      "A test feature",
		DocumentationURL: "https://example.com/docs",
		LicenseURL:       "https://example.com/license",
		Keywords:         []string{"dev", "tools", "testing"},
		LegacyIds:        []string{"old-feature-name", "very-old-name"},
		Deprecated:       true,
		Options: map[string]OptionDefinition{
			"version": {Type: "string", Default: "latest"},
		},
		InstallsAfter: []string{"common-utils"},
		DependsOn:     map[string]interface{}{"base-feature": map[string]interface{}{}},
		ContainerEnv:  map[string]string{"MY_VAR": "value"},
		CapAdd:        []string{"SYS_PTRACE"},
		SecurityOpt:   []string{"seccomp=unconfined"},
		Privileged:    false,
		Init:          true,
	}

	// Verify all fields are set correctly
	assert.Equal(t, "my-feature", metadata.ID)
	assert.Equal(t, "1.2.3", metadata.Version)
	assert.Equal(t, "My Feature", metadata.Name)
	assert.Equal(t, "A test feature", metadata.Description)
	assert.Equal(t, "https://example.com/docs", metadata.DocumentationURL)
	assert.Equal(t, "https://example.com/license", metadata.LicenseURL)
	assert.Equal(t, []string{"dev", "tools", "testing"}, metadata.Keywords)
	assert.Equal(t, []string{"old-feature-name", "very-old-name"}, metadata.LegacyIds)
	assert.True(t, metadata.Deprecated)
	assert.Len(t, metadata.Options, 1)
	assert.Equal(t, []string{"common-utils"}, metadata.InstallsAfter)
	assert.Contains(t, metadata.DependsOn, "base-feature")
	assert.Equal(t, "value", metadata.ContainerEnv["MY_VAR"])
	assert.Equal(t, []string{"SYS_PTRACE"}, metadata.CapAdd)
	assert.Equal(t, []string{"seccomp=unconfined"}, metadata.SecurityOpt)
	assert.False(t, metadata.Privileged)
	assert.True(t, metadata.Init)
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

// TestNormalizeOptionName tests option name normalization per devcontainer spec.
func TestNormalizeOptionName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple lowercase",
			input:    "version",
			expected: "VERSION",
		},
		{
			name:     "camelCase",
			input:    "installZsh",
			expected: "INSTALLZSH",
		},
		{
			name:     "hyphenated option",
			input:    "my-option",
			expected: "MY_OPTION",
		},
		{
			name:     "option with dots",
			input:    "my.option.name",
			expected: "MY_OPTION_NAME",
		},
		{
			name:     "leading digit",
			input:    "2fast",
			expected: "_FAST",
		},
		{
			name:     "leading underscore",
			input:    "_private",
			expected: "_PRIVATE",
		},
		{
			name:     "leading digits and underscores",
			input:    "123_test",
			expected: "_TEST",
		},
		{
			name:     "special characters",
			input:    "foo@bar#baz",
			expected: "FOO_BAR_BAZ",
		},
		{
			name:     "mixed case with hyphen",
			input:    "Install-Oh-My-Zsh",
			expected: "INSTALL_OH_MY_ZSH",
		},
		{
			name:     "already uppercase",
			input:    "VERSION",
			expected: "VERSION",
		},
		{
			name:     "underscores preserved",
			input:    "my_option_name",
			expected: "MY_OPTION_NAME",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeOptionName(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestGetEnvVarsWithNormalization tests that GetEnvVars uses normalized option names.
func TestGetEnvVarsWithNormalization(t *testing.T) {
	feature := &Feature{
		ID: "test-feature",
		Options: map[string]interface{}{
			"my-option":    "value1",
			"another.opt":  "value2",
			"2fast2furious": "value3",
		},
		Metadata: &FeatureMetadata{
			Options: map[string]OptionDefinition{
				"my-option": {
					Type:    "string",
					Default: "default1",
				},
				"another.opt": {
					Type:    "string",
					Default: "default2",
				},
				"2fast2furious": {
					Type:    "string",
					Default: "default3",
				},
			},
		},
	}

	env := feature.GetEnvVars()

	// Options should be normalized
	assert.Equal(t, "value1", env["MY_OPTION"])
	assert.Equal(t, "value2", env["ANOTHER_OPT"])
	assert.Equal(t, "value3", env["_FAST2FURIOUS"])
}
