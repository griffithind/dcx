package features

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func feat(ref, id string, dependsOn []string, installsAfter ...string) *Feature {
	deps := map[string]interface{}{}
	for _, d := range dependsOn {
		deps[d] = map[string]interface{}{}
	}
	return &Feature{ID: ref, Metadata: &FeatureMetadata{ID: id, DependsOn: deps, InstallsAfter: installsAfter}}
}

func keys(fs []*Feature) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = featureKey(f)
	}
	return out
}

// Real metadata references dependencies by full OCI reference while the
// graph is keyed by metadata ID; they used to never match.
func TestOrderFeaturesMatchesFullReferences(t *testing.T) {
	aaa := feat("ghcr.io/acme/features/aaa:1", "aaa", []string{"ghcr.io/devcontainers/features/common-utils:2"})
	utils := feat("ghcr.io/devcontainers/features/common-utils:2", "common-utils", nil)
	zzz := feat("ghcr.io/acme/features/zzz:1", "zzz", nil, "ghcr.io/acme/features/bbb:1")
	bbb := feat("ghcr.io/acme/features/bbb:1", "bbb", nil)

	for _, in := range [][]*Feature{{aaa, utils, zzz, bbb}, {bbb, zzz, utils, aaa}} {
		got, err := OrderFeatures(in, nil)
		require.NoError(t, err)
		k := keys(got)
		assert.Less(t, indexOf(k, "common-utils"), indexOf(k, "aaa"), "dependsOn by full reference: %v", k)
		assert.Less(t, indexOf(k, "bbb"), indexOf(k, "zzz"), "installsAfter by full reference: %v", k)
	}
}

func TestOverrideOrderAcceptsFullReferences(t *testing.T) {
	a := feat("ghcr.io/acme/features/a:1", "a", nil)
	b := feat("ghcr.io/acme/features/b:1", "b", nil)
	c := feat("ghcr.io/acme/features/c:1", "c", nil)
	d := feat("ghcr.io/acme/features/d:1", "d", nil)

	// Listed features come first in the given order; the rest follow in a
	// deterministic order whatever the input order was.
	for _, in := range [][]*Feature{{a, b, c, d}, {d, c, b, a}} {
		got, err := OrderFeatures(in, []string{"ghcr.io/acme/features/c:1", "b"})
		require.NoError(t, err)
		assert.Equal(t, []string{"c", "b", "a", "d"}, keys(got))
	}
}

// installsAfter is an ordering hint; it must not pull in new features.
func TestInstallsAfterDoesNotAddFeatures(t *testing.T) {
	m := &Manager{}
	resolved := map[string]*Feature{
		"a": feat("ghcr.io/acme/features/a:1", "a", []string{"ghcr.io/acme/features/needed:1"}, "ghcr.io/acme/features/optional:1"),
	}
	unresolved := m.collectUnresolvedDependencies(resolved)
	assert.Contains(t, unresolved, "ghcr.io/acme/features/needed:1")
	assert.NotContains(t, unresolved, "ghcr.io/acme/features/optional:1")
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}

// A feature's legacyIds name it in dependency references (S8).
func TestOrderFeaturesHonoursLegacyIDs(t *testing.T) {
	renamed := &Feature{ID: "ghcr.io/x/features/new-name:1", Metadata: &FeatureMetadata{ID: "new-name", LegacyIds: []string{"old-name"}}}
	dependent := &Feature{ID: "ghcr.io/x/features/app:1", Metadata: &FeatureMetadata{ID: "app", DependsOn: map[string]interface{}{"ghcr.io/x/features/old-name:1": map[string]interface{}{}}}}
	ordered, err := OrderFeatures([]*Feature{dependent, renamed}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0] != renamed {
		t.Errorf("the feature named by its legacy ID must install first, got %s first", ordered[0].ID)
	}
	m := &Manager{}
	if unresolved := m.collectUnresolvedDependencies(map[string]*Feature{"new-name": renamed, "app": dependent}); len(unresolved) != 0 {
		t.Errorf("a dependency on a legacy ID must count as resolved: %v", unresolved)
	}
}
