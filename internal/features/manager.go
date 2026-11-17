package features

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/lockfile"
	"github.com/griffithind/dcx/internal/report"
)

// maxFeatureNodes bounds dependency resolution: real feature graphs have a
// handful of nodes, so reaching this means dependsOn never converges.
const maxFeatureNodes = 256

// Manager handles feature resolution, ordering, and installation.
type Manager struct {
	resolver  *Resolver
	configDir string
	lockfile  *lockfile.Lockfile
	report    report.Reporter

	// resolveFeature resolves one feature reference. Defaults to the
	// resolver (honouring the lockfile); overridable in tests.
	resolveFeature func(ctx context.Context, id string, options map[string]interface{}) (*Feature, error)
}

// NewManager creates a new feature manager.
func NewManager(configDir string, rep report.Reporter) (*Manager, error) {
	resolver, err := NewResolver(configDir, rep)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		resolver:  resolver,
		configDir: configDir,
		report:    report.OrDiscard(rep),
	}
	m.resolveFeature = func(ctx context.Context, id string, options map[string]interface{}) (*Feature, error) {
		return m.resolver.ResolveWithLockfile(ctx, id, options, m.lockfile)
	}
	return m, nil
}

// SetForcePull configures the manager to force re-fetch features from the registry.
func (m *Manager) SetForcePull(forcePull bool) {
	m.resolver.SetForcePull(forcePull)
}

// SetLockfile sets the lockfile to use for feature resolution.
// When set, features will be resolved using pinned versions from the lockfile.
func (m *Manager) SetLockfile(lf *lockfile.Lockfile) {
	m.lockfile = lf
}

// request is a feature to resolve: a reference with the options
// devcontainer.json or a feature's dependsOn gives it.
type request struct {
	id      string
	value   interface{}
	options map[string]interface{}
	parent  *node // the feature whose dependsOn asked for it
}

// ResolveAll resolves the features of devcontainer.json and, recursively,
// their dependsOn, and returns them in installation order. It follows
// devcontainers/cli: a feature is identified by its source and options, so
// the same feature asked for with other options is installed again, and
// the order is computed in rounds (see orderFeatures).
func (m *Manager) ResolveAll(ctx context.Context, featuresConfig map[string]interface{}, overrideOrder []string) ([]*Feature, error) {
	if len(featuresConfig) == 0 {
		return nil, nil
	}

	var nodes []*node
	work := m.userRequests(featuresConfig)
	for len(work) > 0 {
		cur := work[0]
		work = work[1:]
		if len(nodes) >= maxFeatureNodes {
			return nil, fmt.Errorf("feature dependencies did not converge after %d features", maxFeatureNodes)
		}
		if mapped, ok := LegacyFeatureRef(cur.id); ok {
			m.report.Warnf("Feature %q is deprecated; use %q", cur.id, strings.TrimSuffix(mapped, ":1"))
		}

		feature, err := m.resolveFeature(ctx, cur.id, cur.options)
		if err != nil {
			if cur.parent != nil {
				return nil, fmt.Errorf("failed to resolve dependency %q of %s: %w", cur.id, cur.parent.feature.ID, err)
			}
			return nil, fmt.Errorf("failed to resolve feature %q: %w", cur.id, err)
		}
		// A string value is the "version" option only for a feature that
		// declares one.
		if _, isString := cur.value.(string); isString && !declaresOption(feature, "version") {
			feature.Options = map[string]interface{}{}
		}
		if cur.parent != nil {
			cur.parent.dependsOn = append(cur.parent.dependsOn, feature)
		}
		if slices.ContainsFunc(nodes, func(n *node) bool { return sameFeature(n.feature, feature) }) {
			continue
		}

		n := &node{feature: feature}
		nodes = append(nodes, n)
		if feature.Metadata == nil {
			continue
		}
		for _, dep := range feature.Metadata.DependsOn {
			work = append(work, request{id: dep.ID, options: dep.Options, parent: n})
		}
		for _, ref := range feature.Metadata.InstallsAfter {
			src, err := ParseFeatureSource(ref)
			if err != nil {
				// The feature's author wrote it; it can order nothing.
				m.report.Warnf("Feature %s: ignoring installsAfter %q: %v", feature.ID, ref, err)
				continue
			}
			n.installsAfter = append(n.installsAfter, softDependency{ref: ref, source: src})
		}
	}

	for _, n := range nodes {
		if n.feature.Metadata != nil && n.feature.Metadata.Deprecated {
			m.report.Warnf("Feature %s is deprecated; see its documentation for a replacement", n.feature.ID)
		}
	}
	ordered, err := m.orderFeatures(ctx, nodes, overrideOrder)
	if err != nil {
		return nil, fmt.Errorf("failed to order features: %w", err)
	}
	return ordered, nil
}

// deprecatedIntoOptions are legacy features that became options of another
// feature (devcontainers/cli's updateDeprecatedFeaturesIntoOptions).
var deprecatedIntoOptions = map[string]struct {
	feature string
	options map[string]interface{}
}{
	"gradle":     {"java", map[string]interface{}{"installGradle": true}},
	"maven":      {"java", map[string]interface{}{"installMaven": true}},
	"jupyterlab": {"python", map[string]interface{}{"installJupyterlab": true}},
}

// userRequests turns devcontainer.json's features into requests, in a
// stable order. A legacy feature that became an option ("maven") sets that
// option on the feature that absorbed it ("java"), which is added when
// devcontainer.json does not list it.
func (m *Manager) userRequests(featuresConfig map[string]interface{}) []request {
	ids := make([]string, 0, len(featuresConfig))
	for id := range featuresConfig {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var reqs []request
	var absorbed []string
	for _, id := range ids {
		if _, ok := deprecatedIntoOptions[id]; ok {
			absorbed = append(absorbed, id)
			continue
		}
		value := featuresConfig[id]
		reqs = append(reqs, request{id: id, value: value, options: featureOptions(value)})
	}
	for _, id := range absorbed {
		into := deprecatedIntoOptions[id]
		qualified := "ghcr.io/devcontainers/features/" + into.feature
		m.report.Warnf("Feature %q is deprecated: it is now part of the %q feature (see https://github.com/devcontainers/features/tree/main/src/%s#options)", id, into.feature, into.feature)
		i := slices.IndexFunc(reqs, func(r request) bool {
			return r.id == into.feature || r.id == qualified || strings.HasPrefix(r.id, qualified+":")
		})
		if i < 0 {
			reqs = append(reqs, request{id: qualified + ":1", options: map[string]interface{}{}})
			i = len(reqs) - 1
		}
		opts := make(map[string]interface{}, len(reqs[i].options)+len(into.options))
		for k, v := range reqs[i].options {
			opts[k] = v
		}
		for k, v := range into.options {
			opts[k] = v
		}
		reqs[i].options, reqs[i].value = opts, opts
	}
	return reqs
}

// declaresOption reports whether the feature's metadata declares name.
func declaresOption(f *Feature, name string) bool {
	if f.Metadata == nil {
		return false
	}
	_, ok := f.Metadata.Options[name]
	return ok
}
