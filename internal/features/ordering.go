package features

import (
	"fmt"
	"sort"
)

// OrderFeatures orders features based on their dependencies.
// It performs a topological sort considering:
// - dependsOn: hard dependencies (must be installed before)
// - installsAfter: soft dependencies (prefer to install after)
// - overrideOrder: explicit ordering from devcontainer.json
func OrderFeatures(features []*Feature, overrideOrder []string) ([]*Feature, error) {
	if len(features) == 0 {
		return features, nil
	}

	// Build a map for quick lookup
	featureMap := make(map[string]*Feature)
	for _, f := range features {
		// Use the feature's metadata ID if available, otherwise use the original ID
		id := f.ID
		if f.Metadata != nil && f.Metadata.ID != "" {
			id = f.Metadata.ID
		}
		featureMap[id] = f
	}

	// If override order is specified, use it
	if len(overrideOrder) > 0 {
		return applyOverrideOrder(features, overrideOrder, featureMap)
	}

	// Build dependency graph
	graph := buildDependencyGraph(features, featureMap)

	// Topological sort
	return topologicalSort(features, graph)
}

// applyOverrideOrder reorders features based on the override order.
func applyOverrideOrder(features []*Feature, overrideOrder []string, featureMap map[string]*Feature) ([]*Feature, error) {
	result := make([]*Feature, 0, len(features))
	used := make(map[string]bool)

	// First, add features in the override order
	for _, id := range overrideOrder {
		if f, ok := featureMap[id]; ok {
			result = append(result, f)
			used[id] = true
		}
	}

	// Then add remaining features not in the override order
	for _, f := range features {
		id := f.ID
		if f.Metadata != nil && f.Metadata.ID != "" {
			id = f.Metadata.ID
		}
		if !used[id] {
			result = append(result, f)
		}
	}

	return result, nil
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
		id := f.ID
		if f.Metadata != nil && f.Metadata.ID != "" {
			id = f.Metadata.ID
		}

		if f.Metadata != nil {
			// Add hard dependencies (iterate over map keys)
			for dep := range f.Metadata.DependsOn {
				graph.hardDeps[id] = append(graph.hardDeps[id], dep)
			}

			// Add soft dependencies
			for _, dep := range f.Metadata.InstallsAfter {
				// Only add soft dep if the dependency is actually in our feature list
				if _, exists := featureMap[dep]; exists {
					graph.softDeps[id] = append(graph.softDeps[id], dep)
				}
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
		id := f.ID
		if f.Metadata != nil && f.Metadata.ID != "" {
			id = f.Metadata.ID
		}
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

// ValidateDependencies checks that all hard dependencies are present.
func ValidateDependencies(features []*Feature) error {
	// Build set of available feature IDs
	available := make(map[string]bool)
	for _, f := range features {
		available[f.ID] = true
		if f.Metadata != nil && f.Metadata.ID != "" {
			available[f.Metadata.ID] = true
		}
	}

	// Check each feature's hard dependencies
	for _, f := range features {
		if f.Metadata == nil {
			continue
		}

		for dep := range f.Metadata.DependsOn {
			if !available[dep] {
				return fmt.Errorf("feature %q requires missing dependency %q", f.ID, dep)
			}
		}
	}

	return nil
}
