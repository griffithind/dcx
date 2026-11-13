package features

import (
	"fmt"
	"sort"
)

// OrderFeatures orders features based on their dependencies.
// It performs a topological sort considering:
//   - dependsOn: hard dependencies (must be installed before)
//   - installsAfter: soft dependencies (prefer to install after)
//   - overrideOrder: explicit ordering from devcontainer.json; features it
//     does not list follow, in dependency order.
//
// Features are identified by one canonical key (featureKey) and every
// reference (dependsOn, installsAfter, overrideOrder) is mapped to the same
// key with normalizeFeatureRef, so "ghcr.io/devcontainers/features/node:1"
// and the metadata ID "node" name the same feature.
func OrderFeatures(features []*Feature, overrideOrder []string) ([]*Feature, error) {
	if len(features) == 0 {
		return features, nil
	}

	featureMap := make(map[string]*Feature, len(features))
	for _, f := range features {
		featureMap[featureKey(f)] = f
	}
	// A feature's legacyIds name it too (references written before a
	// rename keep working).
	for _, f := range features {
		if f.Metadata == nil {
			continue
		}
		for _, legacy := range f.Metadata.LegacyIds {
			if key := normalizeFeatureRef(legacy); featureMap[key] == nil {
				featureMap[key] = f
			}
		}
	}

	var first []*Feature
	used := map[string]bool{}
	for _, ref := range overrideOrder {
		key := canonicalRef(ref, featureMap)
		if f, ok := featureMap[key]; ok && !used[key] {
			first = append(first, f)
			used[key] = true
		}
	}
	var rest []*Feature
	for _, f := range features {
		if !used[featureKey(f)] {
			rest = append(rest, f)
		}
	}

	sorted, err := topologicalSort(rest, buildDependencyGraph(rest, featureMap))
	if err != nil {
		return nil, err
	}
	return append(first, sorted...), nil
}

// canonicalRef maps a reference (full, versioned, or a legacy ID) to the
// featureKey of the feature it names, or to its normalized form when no
// present feature matches.
func canonicalRef(ref string, featureMap map[string]*Feature) string {
	key := normalizeFeatureRef(ref)
	if f := featureMap[key]; f != nil {
		return featureKey(f)
	}
	return key
}

// featureKey is a feature's canonical identity for ordering: its metadata
// ID, or its normalized reference when metadata has none.
func featureKey(f *Feature) string {
	if f.Metadata != nil && f.Metadata.ID != "" {
		return f.Metadata.ID
	}
	return normalizeFeatureRef(f.ID)
}

// dependencyGraph represents the dependency relationships between features.
type dependencyGraph struct {
	// hardDeps maps feature ID to its hard dependencies (dependsOn)
	hardDeps map[string][]string

	// softDeps maps feature ID to its soft dependencies (installsAfter)
	softDeps map[string][]string
}

// buildDependencyGraph constructs the dependency graph from features.
func buildDependencyGraph(features []*Feature, featureMap map[string]*Feature) *dependencyGraph {
	graph := &dependencyGraph{
		hardDeps: make(map[string][]string),
		softDeps: make(map[string][]string),
	}

	for _, f := range features {
		id := featureKey(f)
		if f.Metadata == nil {
			continue
		}
		for dep := range f.Metadata.DependsOn {
			graph.hardDeps[id] = append(graph.hardDeps[id], canonicalRef(dep, featureMap))
		}
		for _, dep := range f.Metadata.InstallsAfter {
			// Soft dependencies only matter for features that are present.
			if featureMap[normalizeFeatureRef(dep)] != nil {
				graph.softDeps[id] = append(graph.softDeps[id], canonicalRef(dep, featureMap))
			}
		}
	}

	return graph
}

// topologicalSort performs a topological sort on the features.
func topologicalSort(features []*Feature, graph *dependencyGraph) ([]*Feature, error) {
	// Build ID list and map
	ids := make([]string, len(features))
	idToFeature := make(map[string]*Feature)
	for i, f := range features {
		id := featureKey(f)
		ids[i] = id
		idToFeature[id] = f
	}

	// Kahn's algorithm for topological sort
	// Calculate in-degree for each node (considering hard deps only for correctness)
	inDegree := make(map[string]int)
	for _, id := range ids {
		inDegree[id] = 0
	}

	// For each feature, count how many hard dependencies it has that are in our set
	for id := range inDegree {
		for _, dep := range graph.hardDeps[id] {
			if _, exists := inDegree[dep]; exists {
				inDegree[id]++
			}
		}
	}

	// Queue for nodes with no dependencies
	var queue []string
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	// Sort queue for deterministic output (considering soft deps)
	sort.Slice(queue, func(i, j int) bool {
		return queue[i] < queue[j]
	})

	// Process queue
	var result []*Feature
	processed := make(map[string]bool)

	for len(queue) > 0 {
		// Pick the best candidate considering soft dependencies
		idx := pickBestCandidate(queue, graph.softDeps, processed)
		current := queue[idx]
		queue = append(queue[:idx], queue[idx+1:]...)

		result = append(result, idToFeature[current])
		processed[current] = true

		// Update in-degrees
		for id, deps := range graph.hardDeps {
			if processed[id] {
				continue
			}
			for _, dep := range deps {
				if dep == current {
					inDegree[id]--
					if inDegree[id] == 0 {
						queue = append(queue, id)
					}
				}
			}
		}

		// Re-sort queue for determinism
		sort.Slice(queue, func(i, j int) bool {
			return queue[i] < queue[j]
		})
	}

	// Check for cycles
	if len(result) != len(features) {
		return nil, fmt.Errorf("circular dependency detected in features")
	}

	return result, nil
}

// pickBestCandidate selects the best candidate from the queue considering soft dependencies.
func pickBestCandidate(queue []string, softDeps map[string][]string, processed map[string]bool) int {
	// Score each candidate: prefer those whose soft dependencies are already processed
	bestIdx := 0
	bestScore := -1

	for i, id := range queue {
		score := 0
		for _, dep := range softDeps[id] {
			if processed[dep] {
				score++
			}
		}
		// Subtract penalty for unprocessed soft deps
		for _, dep := range softDeps[id] {
			if !processed[dep] {
				score--
			}
		}

		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	return bestIdx
}
