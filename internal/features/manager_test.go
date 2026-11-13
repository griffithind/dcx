package features

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeFeatureRef(t *testing.T) {
	tests := map[string]string{
		"ghcr.io/devcontainers/features/node:1":                 "node",
		"ghcr.io/devcontainers/features/node":                   "node",
		"ghcr.io/devcontainers-extra/features/pipx-package:1":   "pipx-package",
		"ghcr.io/devcontainers/features/node@sha256:abcdef0123": "node",
		"localhost:5000/features/thing:2":                       "thing",
		"./local-feature":                                       "local-feature",
		"./local-feature/":                                      "local-feature",
		"https://example.com/releases/feature.tgz":              "feature",
		"common-utils":                                          "common-utils",
	}
	for in, want := range tests {
		assert.Equal(t, want, normalizeFeatureRef(in), in)
	}
}

// A dependsOn key with a version tag must count as resolved once the feature
// is present under its metadata ID. Previously it never matched, so
// resolution looped forever (e.g. devcontainers-extra "black:2" depends on
// "ghcr.io/devcontainers-extra/features/pipx-package:1").
func TestCollectUnresolvedMatchesVersionedDependsOn(t *testing.T) {
	m := &Manager{}
	resolved := map[string]*Feature{
		"black": {
			ID: "ghcr.io/devcontainers-extra/features/black:2",
			Metadata: &FeatureMetadata{
				ID:        "black",
				DependsOn: map[string]interface{}{"ghcr.io/devcontainers-extra/features/pipx-package:1": map[string]interface{}{}},
			},
		},
		"pipx-package": {
			ID:       "ghcr.io/devcontainers-extra/features/pipx-package:1",
			Metadata: &FeatureMetadata{ID: "pipx-package"},
		},
	}
	assert.Empty(t, m.collectUnresolvedDependencies(resolved))
}

func TestResolveDependenciesTerminatesWithVersionedRefs(t *testing.T) {
	var calls atomic.Int32
	m := &Manager{
		resolveFeature: func(_ context.Context, id string, opts map[string]interface{}) (*Feature, error) {
			calls.Add(1)
			switch normalizeFeatureRef(id) {
			case "black":
				return &Feature{ID: id, Options: opts, Metadata: &FeatureMetadata{
					ID:        "black",
					DependsOn: map[string]interface{}{"ghcr.io/devcontainers-extra/features/pipx-package:1": map[string]interface{}{"version": "dep"}},
				}}, nil
			default:
				return &Feature{ID: id, Options: opts, Metadata: &FeatureMetadata{ID: normalizeFeatureRef(id)}}, nil
			}
		},
	}

	done := make(chan struct{})
	var features []*Feature
	var err error
	go func() {
		features, err = m.ResolveAll(context.Background(), map[string]interface{}{
			"ghcr.io/devcontainers-extra/features/black:2": map[string]interface{}{},
		}, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ResolveAll did not terminate")
	}
	require.NoError(t, err)
	assert.Len(t, features, 2)
	assert.Equal(t, int32(2), calls.Load(), "each feature should be resolved exactly once")
}

// A dependency must not replace a feature the user configured explicitly
// (that would silently discard the user's options).
func TestResolveDependenciesKeepsUserOptions(t *testing.T) {
	m := &Manager{
		resolveFeature: func(_ context.Context, id string, opts map[string]interface{}) (*Feature, error) {
			md := &FeatureMetadata{ID: normalizeFeatureRef(id)}
			if md.ID == "a" {
				md.DependsOn = map[string]interface{}{"ghcr.io/x/features/node:1": map[string]interface{}{"version": "18"}}
			}
			return &Feature{ID: id, Options: opts, Metadata: md}, nil
		},
	}

	features, err := m.ResolveAll(context.Background(), map[string]interface{}{
		"ghcr.io/x/features/a:1":    map[string]interface{}{},
		"ghcr.io/x/features/node:1": map[string]interface{}{"version": "22"},
	}, nil)
	require.NoError(t, err)
	for _, f := range features {
		if f.Metadata.ID == "node" {
			assert.Equal(t, "22", f.Options["version"])
		}
	}
}

// A dependency that resolves to a feature whose ID never matches its
// reference must still be attempted only once.
func TestResolveDependenciesAttemptsEachRefOnce(t *testing.T) {
	var calls atomic.Int32
	m := &Manager{
		resolveFeature: func(_ context.Context, id string, opts map[string]interface{}) (*Feature, error) {
			calls.Add(1)
			if id == "root" {
				return &Feature{ID: id, Metadata: &FeatureMetadata{
					ID:        "root",
					DependsOn: map[string]interface{}{"ghcr.io/x/features/misnamed:1": map[string]interface{}{}},
				}}, nil
			}
			// Metadata ID differs from the reference name.
			return &Feature{ID: id, Metadata: &FeatureMetadata{ID: "something-else"}}, nil
		},
	}

	_, err := m.ResolveAll(context.Background(), map[string]interface{}{"root": true}, nil)
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
}

func TestDockerfileEnvQuoteKeepsVariableReferences(t *testing.T) {
	assert.Equal(t, `"/usr/local/go/bin:${PATH}"`, dockerfileEnvQuote("/usr/local/go/bin:${PATH}"))
	assert.Equal(t, `"say \"hi\" \\ bye"`, dockerfileEnvQuote(`say "hi" \ bye`))
	// Option values stay literal.
	assert.Equal(t, `"\$HOME"`, dockerfileQuote("$HOME"))
}

// The generated Dockerfile must be byte-identical for identical inputs (map
// iteration used to reorder ARG/ENV lines and defeat BuildKit's cache), and
// each feature's ENV must follow its own install so later features see it.
func TestDockerfileIsDeterministicWithPerFeatureEnv(t *testing.T) {
	mk := func() []*Feature {
		return []*Feature{
			{ID: "a", Metadata: &FeatureMetadata{ID: "a",
				Options:      map[string]OptionDefinition{"zeta": {Default: "1"}, "alpha": {Default: "2"}, "mid": {Default: "3"}},
				ContainerEnv: map[string]string{"PATH": "/opt/a/bin:${PATH}", "A_HOME": "/opt/a", "Z": "z"}}},
			{ID: "b", Metadata: &FeatureMetadata{ID: "b", ContainerEnv: map[string]string{"B": "1"}}},
		}
	}
	first := NewDockerfileGenerator("alpine:3.20", mk(), "/tmp/x", "", "").Generate()
	for i := 0; i < 20; i++ {
		assert.Equal(t, first, NewDockerfileGenerator("alpine:3.20", mk(), "/tmp/x", "", "").Generate())
	}

	installA := strings.Index(first, "# Feature 1:")
	envA := strings.Index(first, `ENV PATH="/opt/a/bin:${PATH}"`)
	installB := strings.Index(first, "# Feature 2:")
	require.True(t, installA >= 0 && envA > installA && installB > envA, "feature a's ENV must come between its install and feature b's:\n%s", first)
	assert.NotContains(t, first, "# syntax=")
}

// A feature given as a string is its "version" option (F13); it used to
// resolve with no options, silently ignoring the version.
func TestResolveAllStringValueIsVersion(t *testing.T) {
	var got map[string]interface{}
	m := &Manager{}
	m.resolveFeature = func(_ context.Context, id string, options map[string]interface{}) (*Feature, error) {
		got = options
		return &Feature{ID: id, Options: options, Metadata: &FeatureMetadata{ID: "node"}}, nil
	}
	_, err := m.ResolveAll(context.Background(), map[string]interface{}{"ghcr.io/devcontainers/features/node:1": "18"}, nil)
	require.NoError(t, err)
	assert.Equal(t, map[string]interface{}{"version": "18"}, got)
}
