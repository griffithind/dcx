package features

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFeatureSource_OCI(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected FeatureSource
	}{
		{
			name: "ghcr.io with version",
			id:   "ghcr.io/devcontainers/features/go:1",
			expected: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "go",
				Version:    "1",
			},
		},
		{
			name: "references are case-insensitive",
			id:   "GHCR.io/DevContainers/Features/Node:18",
			expected: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "node",
				Version:    "18",
			},
		},
		{
			name: "devcontainers-contrib is devcontainers-extra",
			id:   "ghcr.io/devcontainers-contrib/features/black:2",
			expected: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers-extra/features",
				Resource:   "black",
				Version:    "2",
			},
		},
		{
			name: "a registry with a port",
			id:   "localhost:5000/acme/features/tool:1",
			expected: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "localhost:5000",
				Repository: "acme/features",
				Resource:   "tool",
				Version:    "1",
			},
		},
		{
			name: "legacy short id",
			id:   "golang",
			expected: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "go",
				Version:    "1",
			},
		},
		{
			name: "no version defaults to latest",
			id:   "ghcr.io/devcontainers/features/python",
			expected: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "python",
				Version:    "latest",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseFeatureSource(tt.id)
			require.NoError(t, err)
			assert.Equal(t, tt.expected.Type, ref.Type)
			assert.Equal(t, tt.expected.Registry, ref.Registry)
			assert.Equal(t, tt.expected.Repository, ref.Repository)
			assert.Equal(t, tt.expected.Resource, ref.Resource)
			assert.Equal(t, tt.expected.Version, ref.Version)
		})
	}
}

func TestParseFeatureSource_Local(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseFeatureSource(tt.id)
			require.NoError(t, err)
			assert.Equal(t, SourceTypeLocalPath, ref.Type)
			assert.Equal(t, tt.path, ref.Path)
		})
	}
}

func TestParseFeatureSource_HTTP(t *testing.T) {
	tests := []struct {
		name string
		id   string
		url  string
	}{
		{
			name: "https URL",
			id:   "https://example.com/features/devcontainer-feature-tool.tgz",
			url:  "https://example.com/features/devcontainer-feature-tool.tgz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseFeatureSource(tt.id)
			require.NoError(t, err)
			assert.Equal(t, SourceTypeTarball, ref.Type)
			assert.Equal(t, tt.url, ref.URL)
		})
	}
}

// References devcontainers/cli rejects (F12).
func TestParseFeatureSourceRejects(t *testing.T) {
	for id, want := range map[string]string{
		"devcontainers/features/node:18": "GitHub-release form",
		"fish":                           "legacy feature",
		"/opt/features/custom":           "absolute path",
		"http://example.com/devcontainer-feature-x.tgz":  "https",
		"https://example.com/feature.tgz":                "devcontainer-feature-<id>.tgz",
		"ghcr.io/devcontainers/features/node@sha256:abc": "sha256:<64 hex digits>",
		"ghcr.io/node:1": "<registry>/<namespace>/<feature>",
		"ghcr.io/devcontainers/features/node:bad tag": "bad tag",
	} {
		_, err := ParseFeatureSource(id)
		require.Error(t, err, id)
		assert.Contains(t, err.Error(), want, id)
	}
}

// A tag and a digest: the digest is the version, the tag is kept (it
// orders features), and only localhost is reached over plain http.
func TestParseFeatureSourceTagAndDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ab", 32)
	ref, err := ParseFeatureSource("ghcr.io/devcontainers/features/node:1@" + digest)
	require.NoError(t, err)
	assert.Equal(t, "node", ref.Resource)
	assert.Equal(t, "1", ref.Tag)
	assert.Equal(t, digest, ref.Version)
	assert.Equal(t, "https", ref.scheme())

	local, err := ParseFeatureSource("localhost:5000/acme/features/tool")
	require.NoError(t, err)
	assert.Equal(t, "http", local.scheme())
	assert.Equal(t, "latest", local.Tag)
}

// envVars reads a feature's devcontainer-features.env into a map.
func envVars(f *Feature) map[string]string {
	env := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(f.EnvFile()), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			env[k] = strings.Trim(v, "'")
		}
	}
	return env
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

	env := envVars(feature)

	// Should have user-specified values
	assert.Equal(t, "1.19", env["VERSION"])
	assert.Equal(t, "on", env["MODULES"])

	// Should have default for unspecified option
	assert.Equal(t, "default-value", env["UNUSED"])
}

func TestFeatureSource_CanonicalID(t *testing.T) {
	tests := []struct {
		name     string
		ref      FeatureSource
		expected string
	}{
		{
			name: "OCI reference",
			ref: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "go",
				Version:    "1.19",
			},
			expected: "ghcr.io/devcontainers/features/go:1.19",
		},
		{
			name: "local reference",
			ref: FeatureSource{
				Type: SourceTypeLocalPath,
				Path: "./features/custom",
			},
			expected: "local:./features/custom",
		},
		{
			name: "HTTP reference",
			ref: FeatureSource{
				Type: SourceTypeTarball,
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
		expected FeatureSource
	}{
		{
			name: "common-utils with major version",
			id:   "ghcr.io/devcontainers/features/common-utils:2",
			expected: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "common-utils",
				Version:    "2",
			},
		},
		{
			name: "docker-outside-of-docker",
			id:   "ghcr.io/devcontainers/features/docker-outside-of-docker:1",
			expected: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "docker-outside-of-docker",
				Version:    "1",
			},
		},
		{
			name: "feature with full semver",
			id:   "ghcr.io/devcontainers/features/node:1.2.3",
			expected: FeatureSource{
				Type:       SourceTypeOCI,
				Registry:   "ghcr.io",
				Repository: "devcontainers/features",
				Resource:   "node",
				Version:    "1.2.3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, err := ParseFeatureSource(tt.id)
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
			"username":             "testuser",
			"installZsh":           false,
			"installOhMyZsh":       false,
			"installOhMyZshConfig": false,
			"upgradePackages":      false,
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

	env := envVars(feature)

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

			env := envVars(feature)
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

// TestGetEnvVarsWithNormalization tests that EnvFile uses normalized option names.
func TestGetEnvVarsWithNormalization(t *testing.T) {
	feature := &Feature{
		ID: "test-feature",
		Options: map[string]interface{}{
			"my-option":     "value1",
			"another.opt":   "value2",
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

	env := envVars(feature)

	// Options should be normalized
	assert.Equal(t, "value1", env["MY_OPTION"])
	assert.Equal(t, "value2", env["ANOTHER_OPT"])
	assert.Equal(t, "value3", env["_FAST2FURIOUS"])
}

// Features install as root whatever the base image's USER is (F1), and the
// image's user is restored afterwards.
func TestDockerfileInstallsAsRootAndRestoresTheImageUser(t *testing.T) {
	feats := []*Feature{{ID: "node", Metadata: &FeatureMetadata{ID: "node"}}}
	g := NewDockerfileGenerator("base:1", feats, "/tmp/x", "vscode", "")
	g.SetImageUser("vscode")
	out := g.Generate()

	root := strings.Index(out, "\nUSER root\n")
	install := strings.Index(out, "install.sh")
	restore := strings.LastIndex(out, "\nUSER vscode\n")
	if root < 0 || install < 0 || restore < 0 || root >= install || install >= restore {
		t.Fatalf("want USER root, then the install, then USER vscode; got:\n%s", out)
	}

	g = NewDockerfileGenerator("base:1", feats, "/tmp/x", "", "")
	g.SetImageUser("")
	if out := g.Generate(); strings.Count(out, "\nUSER ") != 1 {
		t.Errorf("a root image needs no restore:\n%s", out)
	}
}

// Digest-pinned references (F12) used to parse into resource "node@sha256"
// and version "<hex>", so the manifest URL and token scope were wrong.
func TestParseFeatureSourceDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("ab", 32)
	ref, err := ParseFeatureSource("ghcr.io/devcontainers/features/node@" + digest)
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io", ref.Registry)
	assert.Equal(t, "devcontainers/features", ref.Repository)
	assert.Equal(t, "node", ref.Resource)
	assert.Equal(t, digest, ref.Version)
	assert.True(t, ref.IsDigest())
	assert.Equal(t, "ghcr.io/devcontainers/features/node@"+digest, ref.CanonicalID())

	tag, err := ParseFeatureSource("ghcr.io/devcontainers/features/node:1")
	require.NoError(t, err)
	assert.False(t, tag.IsDigest())
	assert.Equal(t, "ghcr.io/devcontainers/features/node:1", tag.CanonicalID())
}

// builtin.env resolves homes from the passwd database when the install
// step runs (S5), falling back to the conventional home.
func TestBuiltinEnvResolvesHomes(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs a Linux passwd database")
	}
	me, err := user.Current()
	require.NoError(t, err)
	env := filepath.Join(t.TempDir(), "builtin.env")
	require.NoError(t, os.WriteFile(env, []byte(BuiltinEnv("no-such-user-dcx", me.Username)), 0o644))
	out, err := exec.Command("sh", "-c", `. "$1" && echo "$_REMOTE_USER_HOME|$_CONTAINER_USER_HOME"`, "sh", env).Output()
	require.NoError(t, err)
	assert.Equal(t, "/home/no-such-user-dcx|"+me.HomeDir, strings.TrimSpace(string(out)))
}
