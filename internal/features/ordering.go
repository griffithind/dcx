package features

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/lockfile"
)

// node is a feature in the installation graph (devcontainers/cli's FNode).
type node struct {
	feature *Feature
	// dependsOn are the features its dependsOn resolved to; each must be
	// installed (an equal feature, see sameFeature) in an earlier round.
	dependsOn []*Feature
	// installsAfter are its soft dependencies; those another node
	// satisfies must be installed in an earlier round.
	installsAfter []softDependency
	// priority comes from overrideFeatureInstallOrder: within a round only
	// the nodes with the highest priority are installed.
	priority int
}

// softDependency is an installsAfter (or overrideFeatureInstallOrder)
// reference.
type softDependency struct {
	ref    string
	source FeatureSource
	// aliases are the referenced feature's current id and legacyIds, read
	// from its metadata only when needed (see satisfies).
	aliases       []string
	aliasesLoaded bool
}

// orderFeatures orders the resolved features as devcontainers/cli's
// computeDependsOnInstallationOrder does:
//   - installsAfter references that no feature satisfies are dropped;
//   - each round takes every feature whose dependsOn and installsAfter
//     are installed by earlier rounds (none: a cycle, an error);
//   - overrideFeatureInstallOrder gives its features priorities (the first
//     entry the highest), and a round keeps only its highest-priority
//     features, so the override never breaks a dependency;
//   - a round is sorted by registry and namespace, id, tag, options and
//     digest (compareFeatures).
func (m *Manager) orderFeatures(ctx context.Context, nodes []*node, overrideOrder []string) ([]*Feature, error) {
	for i, ref := range overrideOrder {
		src, err := ParseFeatureSource(ref)
		if err != nil {
			return nil, fmt.Errorf("overrideFeatureInstallOrder: %w", err)
		}
		override := softDependency{ref: ref, source: src, aliasesLoaded: true}
		priority := len(overrideOrder) - i
		matched := false
		for _, n := range nodes {
			ok, err := m.satisfies(ctx, n.feature, &override)
			if err != nil {
				return nil, err
			}
			if ok {
				n.priority = max(n.priority, priority)
				matched = true
			}
		}
		if !matched {
			m.report.Warnf("overrideFeatureInstallOrder lists %q, which is not one of the features", ref)
		}
	}

	// Drop the soft dependencies no feature satisfies.
	for _, n := range nodes {
		kept := n.installsAfter[:0]
		for i := range n.installsAfter {
			ok, err := m.satisfiedByAny(ctx, nodes, &n.installsAfter[i])
			if err != nil {
				return nil, err
			}
			if ok {
				kept = append(kept, n.installsAfter[i])
			}
		}
		n.installsAfter = kept
	}

	var installed []*node
	work := slices.Clone(nodes)
	for len(work) > 0 {
		var round []*node
		for _, n := range work {
			ready, err := m.ready(ctx, n, installed)
			if err != nil {
				return nil, err
			}
			if ready {
				round = append(round, n)
			}
		}
		if len(round) == 0 {
			ids := make([]string, len(work))
			for i, n := range work {
				ids[i] = n.feature.ID
			}
			return nil, fmt.Errorf("circular dependency among features: %s", strings.Join(ids, ", "))
		}
		top := 0
		for _, n := range round {
			top = max(top, n.priority)
		}
		round = slices.DeleteFunc(round, func(n *node) bool { return n.priority != top })
		work = slices.DeleteFunc(work, func(n *node) bool { return slices.Contains(round, n) })
		sort.SliceStable(round, func(i, j int) bool { return compareFeatures(round[i].feature, round[j].feature) < 0 })
		installed = append(installed, round...)
	}

	ordered := make([]*Feature, len(installed))
	for i, n := range installed {
		ordered[i] = n.feature
	}
	return ordered, nil
}

// ready reports whether every dependency of n is installed.
func (m *Manager) ready(ctx context.Context, n *node, installed []*node) (bool, error) {
	for _, dep := range n.dependsOn {
		if !slices.ContainsFunc(installed, func(i *node) bool { return sameFeature(i.feature, dep) }) {
			return false, nil
		}
	}
	for i := range n.installsAfter {
		ok, err := m.satisfiedByAny(ctx, installed, &n.installsAfter[i])
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func (m *Manager) satisfiedByAny(ctx context.Context, nodes []*node, soft *softDependency) (bool, error) {
	for _, n := range nodes {
		ok, err := m.satisfies(ctx, n.feature, soft)
		if err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// satisfies reports whether f is the feature soft refers to, whatever its
// tag, digest or options: for OCI features the same registry, namespace
// and id, or an id the referenced feature lists among its legacyIds (it
// was renamed); for local features the same folder; for tarballs the same
// URL.
func (m *Manager) satisfies(ctx context.Context, f *Feature, soft *softDependency) (bool, error) {
	if f.Source.Type != soft.source.Type {
		return false, nil
	}
	switch f.Source.Type {
	case SourceTypeOCI:
		a, b := f.Source, soft.source
		if a.Registry != b.Registry || a.Repository != b.Repository {
			return false, nil
		}
		if a.Resource == b.Resource {
			return true, nil
		}
		if !soft.aliasesLoaded {
			soft.aliasesLoaded = true
			ref, err := m.resolveFeature(ctx, soft.ref, map[string]interface{}{})
			if err != nil {
				// Only a rename would be missed.
				m.report.Warnf("Could not read the metadata of %s (an installsAfter reference): %v", soft.ref, err)
			} else {
				soft.aliases = aliases(ref)
			}
		}
		return slices.Contains(soft.aliases, a.Resource), nil
	case SourceTypeLocalPath:
		return f.CachePath == filepath.Join(m.configDir, soft.source.Path), nil
	case SourceTypeTarball:
		return f.Source.URL == soft.source.URL, nil
	}
	return false, nil
}

// aliases are the ids a feature is known by: its current id and its
// legacyIds (nil without metadata).
func aliases(f *Feature) []string {
	if f.Metadata == nil {
		return nil
	}
	current := f.Metadata.CurrentID
	if current == "" {
		current = f.Metadata.ID
	}
	return append([]string{current}, f.Metadata.LegacyIds...)
}

// sameFeature reports whether a and b are one feature to install: the same
// source and the same options (devcontainers/cli's equals).
func sameFeature(a, b *Feature) bool {
	return a.Source.Type == b.Source.Type && compareFeatures(a, b) == 0
}

// compareFeatures orders features within a round (devcontainers/cli's
// compareTo): by reference across source types; OCI features by registry
// and namespace, id (an id in common among their aliases is the same id),
// tag, options and manifest digest; local features by folder, then
// options; tarballs by URL, then options. Identical digests and options
// are equal.
func compareFeatures(a, b *Feature) int {
	if a.Source.Type != b.Source.Type {
		return lockfile.CompareIDs(a.ID, b.ID)
	}
	switch a.Source.Type {
	case SourceTypeOCI:
		if a.ManifestDigest == b.ManifestDigest && compareOptions(a.Options, b.Options) == 0 {
			return 0
		}
		if c := compareOCIResource(a, b); c != 0 {
			return c
		}
		if a.Source.Tag != "" && b.Source.Tag != "" && a.Source.Tag != b.Source.Tag {
			return lockfile.CompareIDs(a.Source.Tag, b.Source.Tag)
		}
		if c := compareOptions(a.Options, b.Options); c != 0 {
			return c
		}
		return lockfile.CompareIDs(a.ManifestDigest, b.ManifestDigest)
	case SourceTypeLocalPath:
		if c := lockfile.CompareIDs(a.CachePath, b.CachePath); c != 0 {
			return c
		}
	case SourceTypeTarball:
		if c := lockfile.CompareIDs(a.Source.URL, b.Source.URL); c != 0 {
			return c
		}
	}
	return compareOptions(a.Options, b.Options)
}

func compareOCIResource(a, b *Feature) int {
	an := a.Source.Registry + "/" + a.Source.Repository
	bn := b.Source.Registry + "/" + b.Source.Repository
	if an != bn {
		return lockfile.CompareIDs(an, bn)
	}
	aIDs, bIDs := aliases(a), aliases(b)
	if aIDs == nil {
		aIDs = []string{a.Source.Resource}
	}
	if bIDs == nil {
		bIDs = []string{b.Source.Resource}
	}
	for _, id := range aIDs {
		if slices.Contains(bIDs, id) {
			return 0
		}
	}
	return lockfile.CompareIDs(a.Source.Resource, b.Source.Resource)
}

// compareOptions orders option sets: by size, then key by key, then value
// by value.
func compareOptions(a, b map[string]interface{}) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	ak, bk := sortedOptionKeys(a), sortedOptionKeys(b)
	for i := range ak {
		if ak[i] != bk[i] {
			return lockfile.CompareIDs(ak[i], bk[i])
		}
		av, bv := a[ak[i]], b[bk[i]]
		if c := lockfile.CompareIDs(fmt.Sprintf("%T", av), fmt.Sprintf("%T", bv)); c != 0 {
			return c
		}
		if c := lockfile.CompareIDs(fmt.Sprint(av), fmt.Sprint(bv)); c != 0 {
			return c
		}
	}
	return 0
}

func sortedOptionKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
