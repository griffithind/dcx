package state

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/griffithind/dcx/internal/util"
)

// Label prefix and schema version.
const (
	// Prefix is the namespace prefix for all dcx labels.
	Prefix = "com.griffithind.dcx"

	// SchemaVersion is the current version of the label schema.
	SchemaVersion = "2"

	// LegacyPrefix is the old label prefix for migration.
	LegacyPrefix = "io.github.dcx."
)

// Core identity labels.
const (
	// LabelSchemaVersion identifies the label schema version.
	LabelSchemaVersion = Prefix + ".schema.version"

	// LabelManaged indicates this container is managed by dcx.
	LabelManaged = Prefix + ".managed"

	// LabelWorkspaceID is the stable identifier for the workspace.
	LabelWorkspaceID = Prefix + ".workspace.id"

	// LabelWorkspaceName is the human-readable workspace name.
	LabelWorkspaceName = Prefix + ".workspace.name"

	// LabelWorkspacePath is the absolute path to the workspace.
	LabelWorkspacePath = Prefix + ".workspace.path"

	// LabelConfigPath is the path to devcontainer.json relative to workspace.
	LabelConfigPath = Prefix + ".config.path"
)

// Hash labels for staleness detection.
const (
	// LabelHashConfig is the hash of devcontainer.json content.
	LabelHashConfig = Prefix + ".hash.config"

	// LabelHashDockerfile is the hash of Dockerfile content (if applicable).
	LabelHashDockerfile = Prefix + ".hash.dockerfile"

	// LabelHashCompose is the hash of docker-compose.yml content (if applicable).
	LabelHashCompose = Prefix + ".hash.compose"

	// LabelHashFeatures is the combined hash of all resolved features.
	LabelHashFeatures = Prefix + ".hash.features"

	// LabelHashOverall is the combined hash of all configuration.
	LabelHashOverall = Prefix + ".hash.overall"
)

// State labels.
const (
	// LabelCreatedAt is the RFC3339 timestamp when the container was created.
	LabelCreatedAt = Prefix + ".created.at"

	// LabelCreatedBy is the dcx version that created this container.
	LabelCreatedBy = Prefix + ".created.by"

	// LabelLastStartedAt is the RFC3339 timestamp of last start.
	LabelLastStartedAt = Prefix + ".last.started.at"

	// LabelLifecycleState tracks the container lifecycle state.
	// Values: "created", "ready", "broken"
	LabelLifecycleState = Prefix + ".lifecycle.state"
)

// Lifecycle states.
const (
	LifecycleStateCreated = "created"
	LifecycleStateReady   = "ready"
	LifecycleStateBroken  = "broken"
)

// Feature tracking labels.
const (
	// LabelFeaturesInstalled is a JSON array of installed feature IDs.
	LabelFeaturesInstalled = Prefix + ".features.installed"

	// LabelFeaturesConfig is the JSON of feature options used.
	LabelFeaturesConfig = Prefix + ".features.config"
)

// Build info labels.
const (
	// LabelBaseImage is the original base image reference.
	LabelBaseImage = Prefix + ".build.base.image"

	// LabelDerivedImage is the derived image after features applied.
	LabelDerivedImage = Prefix + ".build.derived.image"

	// LabelBuildMethod indicates how the container was built.
	// Values: "image", "dockerfile", "compose"
	LabelBuildMethod = Prefix + ".build.method"
)

// Build methods.
const (
	BuildMethodImage      = "image"
	BuildMethodDockerfile = "dockerfile"
	BuildMethodCompose    = "compose"
)

// Compose-specific labels.
const (
	// LabelComposeProject is the compose project name.
	LabelComposeProject = Prefix + ".compose.project"

	// LabelComposeService is the service name within the compose project.
	LabelComposeService = Prefix + ".compose.service"

	// LabelIsPrimary indicates this is the primary devcontainer.
	LabelIsPrimary = Prefix + ".container.primary"
)

// Cache labels for persisting data on the container.
const (
	// LabelCacheData is JSON-encoded cache data for staleness checks.
	LabelCacheData = Prefix + ".cache.data"

	// LabelCacheFeatureDigests is JSON map of feature ID to OCI digest.
	LabelCacheFeatureDigests = Prefix + ".cache.feature.digests"

	// LabelCacheProbedEnv is JSON-encoded probed environment variables.
	// Keyed by derived image hash for automatic invalidation on rebuild.
	LabelCacheProbedEnv = Prefix + ".cache.probed.env"

	// LabelCacheProbedEnvHash is the derived image hash used for the cached probe.
	LabelCacheProbedEnvHash = Prefix + ".cache.probed.env.hash"
)

// ContainerLabels represents all dcx labels for a container.
// Renamed from Labels for clarity.
type ContainerLabels struct {
	// Schema version
	SchemaVersion string

	// Identity
	Managed       bool
	WorkspaceID   string
	WorkspaceName string
	WorkspacePath string
	ConfigPath    string

	// Hashes
	HashConfig     string
	HashDockerfile string
	HashCompose    string
	HashFeatures   string
	HashOverall    string

	// State
	CreatedAt      time.Time
	CreatedBy      string
	LastStartedAt  time.Time
	LifecycleState string

	// Features
	FeaturesInstalled []string
	FeaturesConfig    map[string]map[string]interface{}

	// Build info
	BaseImage    string
	DerivedImage string
	BuildMethod  string

	// Compose
	ComposeProject string
	ComposeService string
	IsPrimary      bool

	// Cache
	CacheData           *CacheData
	CacheFeatureDigests map[string]string
	CacheProbedEnv      map[string]string
	CacheProbedEnvHash  string
}

// CacheData holds cached information for staleness detection.
type CacheData struct {
	ConfigHash     string            `json:"config_hash,omitempty"`
	DockerfileHash string            `json:"dockerfile_hash,omitempty"`
	ComposeHash    string            `json:"compose_hash,omitempty"`
	FeaturesHash   string            `json:"features_hash,omitempty"`
	ImageDigest    string            `json:"image_digest,omitempty"`
	FeatureDigests map[string]string `json:"feature_digests,omitempty"`
	LastChecked    time.Time         `json:"last_checked,omitempty"`
}

// NewContainerLabels creates a new ContainerLabels with default values.
func NewContainerLabels() *ContainerLabels {
	return &ContainerLabels{
		SchemaVersion:       SchemaVersion,
		Managed:             true,
		FeaturesInstalled:   []string{},
		FeaturesConfig:      make(map[string]map[string]interface{}),
		CacheFeatureDigests: make(map[string]string),
		CacheProbedEnv:      make(map[string]string),
	}
}

// ToMap converts ContainerLabels to a map for container creation.
func (l *ContainerLabels) ToMap() map[string]string {
	m := map[string]string{
		LabelSchemaVersion: l.SchemaVersion,
		LabelManaged:       util.BoolToString(l.Managed),
	}

	// Identity
	setIfNotEmpty(m, LabelWorkspaceID, l.WorkspaceID)
	setIfNotEmpty(m, LabelWorkspaceName, l.WorkspaceName)
	setIfNotEmpty(m, LabelWorkspacePath, l.WorkspacePath)
	setIfNotEmpty(m, LabelConfigPath, l.ConfigPath)

	// Hashes
	setIfNotEmpty(m, LabelHashConfig, l.HashConfig)
	setIfNotEmpty(m, LabelHashDockerfile, l.HashDockerfile)
	setIfNotEmpty(m, LabelHashCompose, l.HashCompose)
	setIfNotEmpty(m, LabelHashFeatures, l.HashFeatures)
	setIfNotEmpty(m, LabelHashOverall, l.HashOverall)

	// State
	if !l.CreatedAt.IsZero() {
		m[LabelCreatedAt] = l.CreatedAt.Format(time.RFC3339)
	}
	setIfNotEmpty(m, LabelCreatedBy, l.CreatedBy)
	if !l.LastStartedAt.IsZero() {
		m[LabelLastStartedAt] = l.LastStartedAt.Format(time.RFC3339)
	}
	setIfNotEmpty(m, LabelLifecycleState, l.LifecycleState)

	// Features
	if len(l.FeaturesInstalled) > 0 {
		if data, err := json.Marshal(l.FeaturesInstalled); err == nil {
			m[LabelFeaturesInstalled] = string(data)
		}
	}
	if len(l.FeaturesConfig) > 0 {
		if data, err := json.Marshal(l.FeaturesConfig); err == nil {
			m[LabelFeaturesConfig] = string(data)
		}
	}

	// Build info
	setIfNotEmpty(m, LabelBaseImage, l.BaseImage)
	setIfNotEmpty(m, LabelDerivedImage, l.DerivedImage)
	setIfNotEmpty(m, LabelBuildMethod, l.BuildMethod)

	// Compose
	setIfNotEmpty(m, LabelComposeProject, l.ComposeProject)
	setIfNotEmpty(m, LabelComposeService, l.ComposeService)
	if l.IsPrimary {
		m[LabelIsPrimary] = "true"
	}

	// Cache
	if l.CacheData != nil {
		if data, err := json.Marshal(l.CacheData); err == nil {
			m[LabelCacheData] = string(data)
		}
	}
	if len(l.CacheFeatureDigests) > 0 {
		if data, err := json.Marshal(l.CacheFeatureDigests); err == nil {
			m[LabelCacheFeatureDigests] = string(data)
		}
	}
	if len(l.CacheProbedEnv) > 0 {
		if data, err := json.Marshal(l.CacheProbedEnv); err == nil {
			m[LabelCacheProbedEnv] = string(data)
		}
	}
	setIfNotEmpty(m, LabelCacheProbedEnvHash, l.CacheProbedEnvHash)

	return m
}

// ContainerLabelsFromMap creates ContainerLabels from a container label map.
func ContainerLabelsFromMap(m map[string]string) *ContainerLabels {
	l := NewContainerLabels()

	// Schema version
	l.SchemaVersion = m[LabelSchemaVersion]
	l.Managed = m[LabelManaged] == "true"

	// Identity
	l.WorkspaceID = m[LabelWorkspaceID]
	l.WorkspaceName = m[LabelWorkspaceName]
	l.WorkspacePath = m[LabelWorkspacePath]
	l.ConfigPath = m[LabelConfigPath]

	// Hashes
	l.HashConfig = m[LabelHashConfig]
	l.HashDockerfile = m[LabelHashDockerfile]
	l.HashCompose = m[LabelHashCompose]
	l.HashFeatures = m[LabelHashFeatures]
	l.HashOverall = m[LabelHashOverall]

	// State
	if t, err := time.Parse(time.RFC3339, m[LabelCreatedAt]); err == nil {
		l.CreatedAt = t
	}
	l.CreatedBy = m[LabelCreatedBy]
	if t, err := time.Parse(time.RFC3339, m[LabelLastStartedAt]); err == nil {
		l.LastStartedAt = t
	}
	l.LifecycleState = m[LabelLifecycleState]

	// Features
	if data := m[LabelFeaturesInstalled]; data != "" {
		_ = json.Unmarshal([]byte(data), &l.FeaturesInstalled)
	}
	if data := m[LabelFeaturesConfig]; data != "" {
		_ = json.Unmarshal([]byte(data), &l.FeaturesConfig)
	}

	// Build info
	l.BaseImage = m[LabelBaseImage]
	l.DerivedImage = m[LabelDerivedImage]
	l.BuildMethod = m[LabelBuildMethod]

	// Compose
	l.ComposeProject = m[LabelComposeProject]
	l.ComposeService = m[LabelComposeService]
	l.IsPrimary = m[LabelIsPrimary] == "true"

	// Cache
	if data := m[LabelCacheData]; data != "" {
		l.CacheData = &CacheData{}
		_ = json.Unmarshal([]byte(data), l.CacheData)
	}
	if data := m[LabelCacheFeatureDigests]; data != "" {
		_ = json.Unmarshal([]byte(data), &l.CacheFeatureDigests)
	}
	if data := m[LabelCacheProbedEnv]; data != "" {
		_ = json.Unmarshal([]byte(data), &l.CacheProbedEnv)
	}
	l.CacheProbedEnvHash = m[LabelCacheProbedEnvHash]

	return l
}

// IsLegacy returns true if the labels use the legacy prefix.
func IsLegacy(m map[string]string) bool {
	_, hasLegacy := m[LegacyPrefix+"managed"]
	_, hasNew := m[LabelManaged]
	return hasLegacy && !hasNew
}

// MigrateFromLegacy converts legacy labels to the new format.
func MigrateFromLegacy(m map[string]string) *ContainerLabels {
	l := NewContainerLabels()

	// Map legacy labels to new format
	l.Managed = m[LegacyPrefix+"managed"] == "true"
	l.WorkspaceID = m[LegacyPrefix+"env_key"]
	l.WorkspacePath = m[LegacyPrefix+"workspace_path"]
	l.HashConfig = m[LegacyPrefix+"config_hash"]
	l.HashOverall = m[LegacyPrefix+"workspace_root_hash"]

	// Build method from plan
	switch m[LegacyPrefix+"plan"] {
	case "compose":
		l.BuildMethod = BuildMethodCompose
	case "single":
		l.BuildMethod = BuildMethodImage // or dockerfile, we don't know
	}

	l.IsPrimary = m[LegacyPrefix+"primary"] == "true"
	l.ComposeProject = m[LegacyPrefix+"compose_project"]
	l.ComposeService = m[LegacyPrefix+"primary_service"]
	l.WorkspaceName = m[LegacyPrefix+"project_name"]

	// Preserve derived image tag if present
	if imgTag := m[LegacyPrefix+"image_tag"]; imgTag != "" {
		l.DerivedImage = imgTag
	}

	return l
}

// FilterSelector returns a Docker filter string to find dcx containers.
func FilterSelector() string {
	return fmt.Sprintf("label=%s=true", LabelManaged)
}

// FilterByWorkspace returns a Docker filter string to find containers for a workspace.
func FilterByWorkspace(workspaceID string) string {
	return fmt.Sprintf("label=%s=%s", LabelWorkspaceID, workspaceID)
}

// FilterByComposeProject returns a Docker filter for a compose project.
func FilterByComposeProject(project string) string {
	return fmt.Sprintf("label=%s=%s", LabelComposeProject, project)
}

// LegacyFilterSelector returns a filter for finding containers with legacy labels.
func LegacyFilterSelector() string {
	return fmt.Sprintf("label=%smanaged=true", LegacyPrefix)
}

// LegacyFilterByWorkspaceID returns a filter for finding containers by legacy env_key.
func LegacyFilterByWorkspaceID(workspaceID string) string {
	return fmt.Sprintf("label=%senv_key=%s", LegacyPrefix, workspaceID)
}

// IsDCXManaged checks if a container has dcx labels (either new or legacy).
func IsDCXManaged(labelMap map[string]string) bool {
	if labelMap[LabelManaged] == "true" {
		return true
	}
	if labelMap[LegacyPrefix+"managed"] == "true" {
		return true
	}
	return false
}

// GetWorkspaceID extracts the workspace ID from labels (handles both formats).
func GetWorkspaceID(labelMap map[string]string) string {
	if id := labelMap[LabelWorkspaceID]; id != "" {
		return id
	}
	// Fall back to legacy env_key
	return labelMap[LegacyPrefix+"env_key"]
}

// GetWorkspacePath extracts the workspace path from labels (handles both formats).
func GetWorkspacePath(labelMap map[string]string) string {
	if path := labelMap[LabelWorkspacePath]; path != "" {
		return path
	}
	return labelMap[LegacyPrefix+"workspace_path"]
}

// IsPrimaryContainer checks if container is primary (handles both formats).
func IsPrimaryContainer(labelMap map[string]string) bool {
	if labelMap[LabelIsPrimary] == "true" {
		return true
	}
	return labelMap[LegacyPrefix+"primary"] == "true"
}

// Helper functions

func setIfNotEmpty(m map[string]string, key, value string) {
	if value != "" {
		m[key] = value
	}
}
