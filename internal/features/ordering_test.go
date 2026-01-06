package features

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderFeatures_NoDependencies(t *testing.T) {
	features := []*Feature{
		{ID: "feature-a", Metadata: &FeatureMetadata{ID: "a"}},
		{ID: "feature-b", Metadata: &FeatureMetadata{ID: "b"}},
		{ID: "feature-c", Metadata: &FeatureMetadata{ID: "c"}},
	}

	ordered, err := OrderFeatures(features, nil)
	require.NoError(t, err)
	assert.Len(t, ordered, 3)
}

func TestOrderFeatures_HardDependencies(t *testing.T) {
	features := []*Feature{
		{ID: "feature-a", Metadata: &FeatureMetadata{ID: "a", DependsOn: []string{"b"}}},
		{ID: "feature-b", Metadata: &FeatureMetadata{ID: "b"}},
		{ID: "feature-c", Metadata: &FeatureMetadata{ID: "c", DependsOn: []string{"a"}}},
	}

	ordered, err := OrderFeatures(features, nil)
	require.NoError(t, err)
	require.Len(t, ordered, 3)

	// b should come before a (a depends on b)
	bIdx := findFeatureIndex(ordered, "feature-b")
	aIdx := findFeatureIndex(ordered, "feature-a")
	cIdx := findFeatureIndex(ordered, "feature-c")

	assert.Less(t, bIdx, aIdx, "b should come before a")
	assert.Less(t, aIdx, cIdx, "a should come before c")
}

func TestOrderFeatures_SoftDependencies(t *testing.T) {
	features := []*Feature{
		{ID: "feature-a", Metadata: &FeatureMetadata{ID: "a", InstallsAfter: []string{"b"}}},
		{ID: "feature-b", Metadata: &FeatureMetadata{ID: "b"}},
	}

	ordered, err := OrderFeatures(features, nil)
	require.NoError(t, err)
	require.Len(t, ordered, 2)

	// b should preferably come before a (soft dependency)
	bIdx := findFeatureIndex(ordered, "feature-b")
	aIdx := findFeatureIndex(ordered, "feature-a")

	assert.Less(t, bIdx, aIdx, "b should come before a (soft dep)")
}

func TestOrderFeatures_OverrideOrder(t *testing.T) {
	features := []*Feature{
		{ID: "feature-a", Metadata: &FeatureMetadata{ID: "a"}},
		{ID: "feature-b", Metadata: &FeatureMetadata{ID: "b"}},
		{ID: "feature-c", Metadata: &FeatureMetadata{ID: "c"}},
	}

	overrideOrder := []string{"c", "a", "b"}

	ordered, err := OrderFeatures(features, overrideOrder)
	require.NoError(t, err)
	require.Len(t, ordered, 3)

	// Should follow override order
	assert.Equal(t, "feature-c", ordered[0].ID)
	assert.Equal(t, "feature-a", ordered[1].ID)
	assert.Equal(t, "feature-b", ordered[2].ID)
}

func TestOrderFeatures_CycleDetection(t *testing.T) {
	features := []*Feature{
		{ID: "feature-a", Metadata: &FeatureMetadata{ID: "a", DependsOn: []string{"b"}}},
		{ID: "feature-b", Metadata: &FeatureMetadata{ID: "b", DependsOn: []string{"a"}}},
	}

	_, err := OrderFeatures(features, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestValidateDependencies_MissingDep(t *testing.T) {
	features := []*Feature{
		{ID: "feature-a", Metadata: &FeatureMetadata{ID: "a", DependsOn: []string{"missing"}}},
	}

	err := ValidateDependencies(features)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing dependency")
}

func TestValidateDependencies_AllPresent(t *testing.T) {
	features := []*Feature{
		{ID: "feature-a", Metadata: &FeatureMetadata{ID: "a", DependsOn: []string{"b"}}},
		{ID: "feature-b", Metadata: &FeatureMetadata{ID: "b"}},
	}

	err := ValidateDependencies(features)
	assert.NoError(t, err)
}

func findFeatureIndex(features []*Feature, id string) int {
	for i, f := range features {
		if f.ID == id {
			return i
		}
	}
	return -1
}
