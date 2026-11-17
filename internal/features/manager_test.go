package features

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// catalog is an in-memory feature source for ResolveAll: metadata by
// "registry/namespace/id" (OCI) or local path.
type catalog map[string]*FeatureMetadata

const testConfigDir = "/ws/.devcontainer"

func (c catalog) manager(rep report.Reporter) *Manager {
	m := &Manager{configDir: testConfigDir, report: report.OrDiscard(rep)}
	m.resolveFeature = func(_ context.Context, id string, opts map[string]interface{}) (*Feature, error) {
		src, err := ParseFeatureSource(id)
		if err != nil {
			return nil, err
		}
		key := src.Registry + "/" + src.Repository + "/" + src.Resource
		if src.Type == SourceTypeLocalPath {
			key = src.Path
		}
		md, ok := c[key]
		if !ok {
			return nil, fmt.Errorf("%s not found", key)
		}
		f := &Feature{ID: id, Source: src, Options: opts, Metadata: md, ManifestDigest: "sha256:" + key}
		if src.Type == SourceTypeLocalPath {
			f.CachePath = filepath.Join(testConfigDir, src.Path)
		}
		return f, nil
	}
	return m
}

func (c catalog) resolve(t *testing.T, features map[string]interface{}, override ...string) []*Feature {
	t.Helper()
	got, err := c.manager(nil).ResolveAll(context.Background(), features, override)
	require.NoError(t, err)
	return got
}

// order lists each feature as its reference and options.
func order(fs []*Feature) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.ID
		if len(f.Options) > 0 {
			out[i] += fmt.Sprint(f.Options)
		}
	}
	return out
}

const (
	utilsRef  = "ghcr.io/devcontainers/features/common-utils:2"
	nodeRef   = "ghcr.io/devcontainers/features/node:1"
	pythonRef = "ghcr.io/devcontainers/features/python:1"
)

func standard() catalog {
	return catalog{
		"ghcr.io/devcontainers/features/common-utils": {ID: "common-utils", Options: map[string]OptionDefinition{"username": {Type: "string", Default: "automatic"}}},
		"ghcr.io/devcontainers/features/node":         {ID: "node", InstallsAfter: []string{"ghcr.io/devcontainers/features/common-utils"}, Options: map[string]OptionDefinition{"version": {Type: "string", Default: "lts"}}},
		"ghcr.io/devcontainers/features/python":       {ID: "python", InstallsAfter: []string{"ghcr.io/devcontainers/features/common-utils"}},
		"ghcr.io/devcontainers/features/java":         {ID: "java"},
		"ghcr.io/acme/features/aaa":                   {ID: "aaa"},
		"ghcr.io/acme/features/zzz":                   {ID: "zzz"},
	}
}

// Rounds: first the features without dependencies (common-utils and zzz,
// sorted by namespace: acme before devcontainers), then node and python,
// which install after common-utils.
func TestResolveAllOrdersInRounds(t *testing.T) {
	got := standard().resolve(t, map[string]interface{}{pythonRef: true, nodeRef: true, utilsRef: true, "ghcr.io/acme/features/zzz:1": true})
	assert.Equal(t, []string{"ghcr.io/acme/features/zzz:1", utilsRef, nodeRef, pythonRef}, order(got))
}

// installsAfter is a hard constraint among listed features (F7): zzz sorts
// after aaa but must install first.
func TestInstallsAfterIsHonoured(t *testing.T) {
	c := standard()
	c["ghcr.io/acme/features/aaa"] = &FeatureMetadata{ID: "aaa", InstallsAfter: []string{"ghcr.io/acme/features/zzz"}}
	got := c.resolve(t, map[string]interface{}{"ghcr.io/acme/features/aaa:1": true, "ghcr.io/acme/features/zzz:1": true})
	assert.Equal(t, []string{"ghcr.io/acme/features/zzz:1", "ghcr.io/acme/features/aaa:1"}, order(got))
}

// installsAfter never adds a feature, and a reference that is no feature
// reference is ignored.
func TestInstallsAfterDoesNotAddFeatures(t *testing.T) {
	got := standard().resolve(t, map[string]interface{}{nodeRef: true})
	assert.Equal(t, []string{nodeRef}, order(got))

	c := standard()
	c["ghcr.io/acme/features/aaa"] = &FeatureMetadata{ID: "aaa", InstallsAfter: []string{"simple-marker"}}
	got = c.resolve(t, map[string]interface{}{"ghcr.io/acme/features/aaa:1": true})
	assert.Equal(t, []string{"ghcr.io/acme/features/aaa:1"}, order(got))
}

// dependsOn adds the dependency, which installs first.
func TestDependsOnAddsAndOrders(t *testing.T) {
	c := standard()
	c["ghcr.io/acme/features/aaa"] = &FeatureMetadata{ID: "aaa", DependsOn: Dependencies{{ID: "ghcr.io/acme/features/zzz:1", Options: map[string]interface{}{}}}}
	got := c.resolve(t, map[string]interface{}{"ghcr.io/acme/features/aaa:1": true})
	assert.Equal(t, []string{"ghcr.io/acme/features/zzz:1", "ghcr.io/acme/features/aaa:1"}, order(got))
}

// overrideFeatureInstallOrder is a priority within rounds (F6): it orders
// independent features, but never puts a feature before its dependency.
func TestOverrideOrderIsAPriority(t *testing.T) {
	c := standard()
	got := c.resolve(t, map[string]interface{}{"ghcr.io/acme/features/aaa:1": true, "ghcr.io/acme/features/zzz:1": true, "ghcr.io/devcontainers/features/java:1": true},
		"ghcr.io/acme/features/zzz", "ghcr.io/devcontainers/features/java")
	assert.Equal(t, []string{"ghcr.io/acme/features/zzz:1", "ghcr.io/devcontainers/features/java:1", "ghcr.io/acme/features/aaa:1"}, order(got))

	c["ghcr.io/acme/features/aaa"] = &FeatureMetadata{ID: "aaa", DependsOn: Dependencies{{ID: "ghcr.io/acme/features/zzz:1", Options: map[string]interface{}{}}}}
	got = c.resolve(t, map[string]interface{}{"ghcr.io/acme/features/aaa:1": true}, "ghcr.io/acme/features/aaa")
	assert.Equal(t, []string{"ghcr.io/acme/features/zzz:1", "ghcr.io/acme/features/aaa:1"}, order(got))
}

func TestOverrideOrderWarnsAboutAMissingFeature(t *testing.T) {
	rec := &report.Recorder{}
	_, err := standard().manager(rec).ResolveAll(context.Background(), map[string]interface{}{nodeRef: true}, []string{"ghcr.io/acme/features/missing"})
	require.NoError(t, err)
	assert.Contains(t, fmt.Sprint(rec.Messages()), "missing")

	_, err = standard().manager(nil).ResolveAll(context.Background(), map[string]interface{}{nodeRef: true}, []string{"node-without-registry/x"})
	require.Error(t, err, "an entry that is no feature reference is an error")
}

func TestResolveAllDetectsCycles(t *testing.T) {
	c := standard()
	c["ghcr.io/acme/features/aaa"] = &FeatureMetadata{ID: "aaa", DependsOn: Dependencies{{ID: "ghcr.io/acme/features/zzz:1", Options: map[string]interface{}{}}}}
	c["ghcr.io/acme/features/zzz"] = &FeatureMetadata{ID: "zzz", DependsOn: Dependencies{{ID: "ghcr.io/acme/features/aaa:1", Options: map[string]interface{}{}}}}
	_, err := c.manager(nil).ResolveAll(context.Background(), map[string]interface{}{"ghcr.io/acme/features/aaa:1": true}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

// A feature is its source and options (F9): a dependency asked for with
// other options than the user's is installed again with them; asked for
// twice with the same options, once.
func TestFeatureIdentityIsSourceAndOptions(t *testing.T) {
	c := standard()
	dep := func(opts map[string]interface{}) Dependencies {
		return Dependencies{{ID: nodeRef, Options: opts}}
	}
	c["ghcr.io/acme/features/aaa"] = &FeatureMetadata{ID: "aaa", DependsOn: dep(map[string]interface{}{"version": "18"})}
	c["ghcr.io/acme/features/zzz"] = &FeatureMetadata{ID: "zzz", DependsOn: dep(map[string]interface{}{"version": "18"})}
	got := c.resolve(t, map[string]interface{}{
		"ghcr.io/acme/features/aaa:1": true, "ghcr.io/acme/features/zzz:1": true,
		nodeRef: map[string]interface{}{"version": "22"},
	})
	assert.Equal(t, []string{nodeRef + "map[version:18]", nodeRef + "map[version:22]", "ghcr.io/acme/features/aaa:1", "ghcr.io/acme/features/zzz:1"}, order(got))
}

// A local feature named like a registry feature is another feature (F9).
func TestLocalFeatureDoesNotSatisfyARegistryDependency(t *testing.T) {
	c := standard()
	c["./node"] = &FeatureMetadata{ID: "node"}
	c["ghcr.io/acme/features/aaa"] = &FeatureMetadata{ID: "aaa", DependsOn: Dependencies{{ID: nodeRef, Options: map[string]interface{}{}}}}
	got := c.resolve(t, map[string]interface{}{"./node": true, "ghcr.io/acme/features/aaa:1": true})
	assert.ElementsMatch(t, []string{"./node", nodeRef, "ghcr.io/acme/features/aaa:1"}, order(got))
}

// An installsAfter reference to a renamed feature is satisfied by its old
// name (the referenced feature lists it in legacyIds).
func TestInstallsAfterFollowsRenames(t *testing.T) {
	c := standard()
	c["ghcr.io/acme/features/new"] = &FeatureMetadata{ID: "new", LegacyIds: []string{"old"}}
	c["ghcr.io/acme/features/old"] = &FeatureMetadata{ID: "old"}
	c["ghcr.io/acme/features/aaa"] = &FeatureMetadata{ID: "aaa", InstallsAfter: []string{"ghcr.io/acme/features/new"}}
	got := c.resolve(t, map[string]interface{}{"ghcr.io/acme/features/aaa:1": true, "ghcr.io/acme/features/old:1": true})
	assert.Equal(t, []string{"ghcr.io/acme/features/old:1", "ghcr.io/acme/features/aaa:1"}, order(got))
}

// Legacy short ids map to ghcr.io/devcontainers/features (F11), and
// features folded into another become its options.
func TestLegacyFeatureIDs(t *testing.T) {
	rec := &report.Recorder{}
	got, err := standard().manager(rec).ResolveAll(context.Background(), map[string]interface{}{
		"node": true, "maven": true, "ghcr.io/devcontainers/features/java:1": map[string]interface{}{"version": "17"},
	}, nil)
	require.NoError(t, err)
	require.Len(t, got, 2)
	byID := map[string]*Feature{}
	for _, f := range got {
		byID[f.ID] = f
	}
	require.Contains(t, byID, "node")
	assert.Equal(t, "ghcr.io", byID["node"].Source.Registry)
	assert.Equal(t, "1", byID["node"].Source.Tag)
	assert.Equal(t, map[string]interface{}{"version": "17", "installMaven": true}, byID["ghcr.io/devcontainers/features/java:1"].Options)
	assert.Contains(t, fmt.Sprint(rec.Messages()), "deprecated")

	got = standard().resolve(t, map[string]interface{}{"maven": true})
	assert.Equal(t, []string{"ghcr.io/devcontainers/features/java:1map[installMaven:true]"}, order(got))

	_, err = standard().manager(nil).ResolveAll(context.Background(), map[string]interface{}{"fish": true}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "legacy feature")
}

// A feature set to false still installs, with its defaults (F16), as
// devcontainers/cli does.
func TestFalseFeatureInstallsWithDefaults(t *testing.T) {
	got := standard().resolve(t, map[string]interface{}{nodeRef: false})
	assert.Equal(t, []string{nodeRef}, order(got))
}

// A string is the "version" option of a feature that declares one, and
// nothing for one that doesn't.
func TestStringValueIsVersion(t *testing.T) {
	got := standard().resolve(t, map[string]interface{}{nodeRef: "18", pythonRef: "3.12"})
	assert.Equal(t, []string{nodeRef + "map[version:18]", pythonRef}, order(got))
}

func TestDockerfileEnvQuoteKeepsVariableReferences(t *testing.T) {
	assert.Equal(t, `"/usr/local/go/bin:${PATH}"`, dockerfileEnvQuote("/usr/local/go/bin:${PATH}"))
	assert.Equal(t, `"say \"hi\" \\ bye"`, dockerfileEnvQuote(`say "hi" \ bye`))
}

// The generated Dockerfile must be byte-identical for identical inputs (map
// iteration used to reorder ENV lines and defeat BuildKit's cache), each
// feature's ENV comes before its own install (F4), and options are no
// build ARGs, which leaked into every later feature (F5).
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

	envA := strings.Index(first, `ENV PATH="/opt/a/bin:${PATH}"`)
	installA := strings.Index(first, "# Feature 1:")
	envB := strings.Index(first, `ENV B="1"`)
	installB := strings.Index(first, "# Feature 2:")
	require.True(t, envA >= 0 && installA > envA && envB > installA && installB > envB, "each feature's ENV must come right before its install:\n%s", first)
	assert.NotContains(t, first, "ARG ZETA")
	assert.Contains(t, first, ". ./devcontainer-features.env && set +a")
	assert.NotContains(t, first, "# syntax=")
}

// The env file has every default plus every option the user set, declared
// or not (F5), shell-quoted.
func TestEnvFile(t *testing.T) {
	f := &Feature{
		Options: map[string]interface{}{"version": "18", "extra-thing": true, "path": "$HOME/x y"},
		Metadata: &FeatureMetadata{Options: map[string]OptionDefinition{
			"version": {Default: "lts"}, "installYarn": {Default: true}, "nodeGypDependencies": {},
		}},
	}
	assert.Equal(t, "EXTRA_THING=true\nINSTALLYARN=true\nPATH='$HOME/x y'\nVERSION=18\n", f.EnvFile())
}

func TestFeatureUsers(t *testing.T) {
	for _, tc := range []struct {
		remote, container string
		fallbacks         []string
		wantRemote        string
		wantContainer     string
	}{
		{"", "", nil, "root", "root"},
		{"", "", []string{"", "node"}, "node", "node"},  // the image's USER
		{"", "", []string{"app", "node"}, "app", "app"}, // the compose service's user first
		{"vscode", "", []string{"", "1000:1000"}, "vscode", "1000"},
		{"", "dev", []string{"app", "node"}, "dev", "dev"},
	} {
		remote, container := FeatureUsers(tc.remote, tc.container, tc.fallbacks...)
		assert.Equal(t, tc.wantRemote, remote, "%+v", tc)
		assert.Equal(t, tc.wantContainer, container, "%+v", tc)
	}
}
