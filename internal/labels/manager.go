package labels

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// Manager handles label operations for dcx containers.
type Manager struct {
	docker  client.APIClient
	version string
	logger  *slog.Logger
}

// NewManager creates a new label manager.
func NewManager(dockerClient client.APIClient, logger *slog.Logger) *Manager {
	return &Manager{
		docker:  dockerClient,
		version: SchemaVersion,
		logger:  logger,
	}
}

// Build creates labels for a new container.
func (m *Manager) Build(opts BuildOptions) *Labels {
	l := NewLabels()

	// Identity
	l.WorkspaceID = opts.WorkspaceID
	l.WorkspaceName = opts.WorkspaceName
	l.WorkspacePath = opts.WorkspacePath
	l.ConfigPath = opts.ConfigPath

	// Hashes
	l.HashConfig = opts.HashConfig
	l.HashDockerfile = opts.HashDockerfile
	l.HashCompose = opts.HashCompose
	l.HashFeatures = opts.HashFeatures
	l.HashOverall = opts.HashOverall

	// State
	l.CreatedAt = time.Now()
	l.CreatedBy = opts.DCXVersion
	l.LifecycleState = LifecycleStateCreated

	// Features
	l.FeaturesInstalled = opts.FeaturesInstalled
	l.FeaturesConfig = opts.FeaturesConfig

	// Build info
	l.BaseImage = opts.BaseImage
	l.DerivedImage = opts.DerivedImage
	l.BuildMethod = opts.BuildMethod

	// Compose
	l.ComposeProject = opts.ComposeProject
	l.ComposeService = opts.ComposeService
	l.IsPrimary = opts.IsPrimary

	// Initialize cache data
	l.CacheData = &CacheData{
		ConfigHash:     opts.HashConfig,
		DockerfileHash: opts.HashDockerfile,
		ComposeHash:    opts.HashCompose,
		FeaturesHash:   opts.HashFeatures,
		LastChecked:    time.Now(),
	}
	l.CacheFeatureDigests = opts.FeatureDigests

	return l
}

// BuildOptions contains options for building container labels.
type BuildOptions struct {
	// Identity
	WorkspaceID   string
	WorkspaceName string
	WorkspacePath string
	ConfigPath    string
	DCXVersion    string

	// Hashes
	HashConfig     string
	HashDockerfile string
	HashCompose    string
	HashFeatures   string
	HashOverall    string

	// Features
	FeaturesInstalled []string
	FeaturesConfig    map[string]map[string]interface{}
	FeatureDigests    map[string]string

	// Build info
	BaseImage    string
	DerivedImage string
	BuildMethod  string

	// Compose
	ComposeProject string
	ComposeService string
	IsPrimary      bool
}

// Read retrieves and parses labels from a container.
func (m *Manager) Read(ctx context.Context, containerID string) (*Labels, error) {
	info, err := m.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspect container: %w", err)
	}

	labelMap := info.Config.Labels

	// Check if migration is needed
	if IsLegacy(labelMap) {
		m.logger.Info("detected legacy labels, migrating",
			"container", containerID)
		return MigrateFromLegacy(labelMap), nil
	}

	return FromMap(labelMap), nil
}

// ReadFromMap parses labels from a map (useful when you already have container info).
func (m *Manager) ReadFromMap(labelMap map[string]string) *Labels {
	if IsLegacy(labelMap) {
		return MigrateFromLegacy(labelMap)
	}
	return FromMap(labelMap)
}


// GetCache retrieves cached data from container labels.
func (m *Manager) GetCache(ctx context.Context, containerID string) (*CacheData, error) {
	labels, err := m.Read(ctx, containerID)
	if err != nil {
		return nil, err
	}
	return labels.CacheData, nil
}

// CheckStaleness compares current hashes with cached hashes on the container.
func (m *Manager) CheckStaleness(ctx context.Context, containerID string, current *ConfigHashes) (*StalenessResult, error) {
	labels, err := m.Read(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("read labels: %w", err)
	}

	result := &StalenessResult{
		Changes: []string{},
	}

	// No cache data means we can't determine staleness
	if labels.CacheData == nil {
		result.IsStale = true
		result.Reason = "no cache data on container"
		return result, nil
	}

	cache := labels.CacheData

	// Compare each hash
	if current.Config != "" && cache.ConfigHash != "" && current.Config != cache.ConfigHash {
		result.IsStale = true
		result.Changes = append(result.Changes, "devcontainer.json changed")
	}

	if current.Dockerfile != "" && cache.DockerfileHash != "" && current.Dockerfile != cache.DockerfileHash {
		result.IsStale = true
		result.Changes = append(result.Changes, "Dockerfile changed")
	}

	if current.Compose != "" && cache.ComposeHash != "" && current.Compose != cache.ComposeHash {
		result.IsStale = true
		result.Changes = append(result.Changes, "docker-compose.yml changed")
	}

	if current.Features != "" && cache.FeaturesHash != "" && current.Features != cache.FeaturesHash {
		result.IsStale = true
		result.Changes = append(result.Changes, "features changed")
	}

	if result.IsStale {
		result.Reason = fmt.Sprintf("configuration changed: %v", result.Changes)
	}

	return result, nil
}

// ConfigHashes contains hashes for staleness detection.
type ConfigHashes struct {
	Config     string
	Dockerfile string
	Compose    string
	Features   string
	Overall    string
}

// StalenessResult contains the result of a staleness check.
type StalenessResult struct {
	IsStale bool
	Reason  string
	Changes []string
}

// MergeLabels merges additional labels into a label map without overwriting existing dcx labels.
func MergeLabels(base map[string]string, additional map[string]string) map[string]string {
	result := make(map[string]string)

	// Copy base labels
	for k, v := range base {
		result[k] = v
	}

	// Add additional labels that don't conflict with dcx namespace
	for k, v := range additional {
		// Don't overwrite dcx labels
		if len(k) >= len(Prefix) && k[:len(Prefix)] == Prefix {
			continue
		}
		result[k] = v
	}

	return result
}

// ListContainers returns all dcx-managed containers.
func (m *Manager) ListContainers(ctx context.Context) ([]ContainerInfo, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("%s=true", LabelManaged))

	containers, err := m.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, c := range containers {
		labels := m.ReadFromMap(c.Labels)
		result = append(result, ContainerInfo{
			ID:     c.ID,
			Names:  c.Names,
			State:  c.State,
			Labels: labels,
		})
	}

	return result, nil
}

// ContainerInfo holds basic container info with parsed labels.
type ContainerInfo struct {
	ID     string
	Names  []string
	State  string
	Labels *Labels
}

// ListByWorkspace returns containers for a specific workspace.
func (m *Manager) ListByWorkspace(ctx context.Context, workspaceID string) ([]ContainerInfo, error) {
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", fmt.Sprintf("%s=%s", LabelWorkspaceID, workspaceID))

	containers, err := m.docker.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	result := make([]ContainerInfo, 0, len(containers))
	for _, c := range containers {
		labels := m.ReadFromMap(c.Labels)
		result = append(result, ContainerInfo{
			ID:     c.ID,
			Names:  c.Names,
			State:  c.State,
			Labels: labels,
		})
	}

	return result, nil
}

// FindPrimaryContainer finds the primary container for a workspace.
func (m *Manager) FindPrimaryContainer(ctx context.Context, workspaceID string) (*ContainerInfo, error) {
	containers, err := m.ListByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	for _, c := range containers {
		if c.Labels.IsPrimary {
			return &c, nil
		}
	}

	return nil, nil
}

// FindPrimaryContainerWithFallback finds the primary container, trying primary ID first then fallback.
// This is useful when containers may be labeled with either a project name or workspace hash.
func (m *Manager) FindPrimaryContainerWithFallback(ctx context.Context, primaryID, fallbackID string) (*ContainerInfo, error) {
	// Try primary ID first (usually project name)
	if primaryID != "" {
		container, err := m.FindPrimaryContainer(ctx, primaryID)
		if err != nil {
			return nil, err
		}
		if container != nil {
			return container, nil
		}
	}

	// Try fallback ID (usually workspace hash)
	if fallbackID != "" && fallbackID != primaryID {
		return m.FindPrimaryContainer(ctx, fallbackID)
	}

	return nil, nil
}
