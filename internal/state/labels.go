package state

import (
	"encoding/json"
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
	// LabelHashConfig is the combined hash of all build inputs
	// (devcontainer.json, Dockerfiles, compose files, features).
	LabelHashConfig = Prefix + ".hash.config"
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

// SSH-related labels.
//
// Labels are immutable after container creation (Docker has no label-update
// API), so these are stamped once at create time and read by all subsequent
// dcx invocations. The port is chosen by Docker (-p 127.0.0.1::48022) and
// read back via `docker port`, so the value here is authoritative after the
// container exists.
const (
	// LabelSSHHostPort is the ephemeral host port Docker mapped to the
	// agent's :48022 listener, written as a decimal string.
	LabelSSHHostPort = Prefix + ".ssh.host.port"

	// LabelSSHBindAddress is the host interface the port is bound to,
	// typically "127.0.0.1" or (with --hosts) "0.0.0.0".
	LabelSSHBindAddress = Prefix + ".ssh.bind.address"

	// LabelSSHAllowedClientIPs is a comma-separated list of CIDR strings
	// (plus bare IPs) the agent's ConnCallback will accept in addition to
	// loopback. Empty means loopback-only.
	LabelSSHAllowedClientIPs = Prefix + ".ssh.allowed.client.ips"

	// LabelSSHAuthorizedKeysSHA256 is the hex-encoded SHA256 of the content
	// that was written to /run/secrets/dcx/authorized_keys at create time.
	// A mismatch on subsequent Up() triggers silent re-sync.
	LabelSSHAuthorizedKeysSHA256 = Prefix + ".ssh.authorized.keys.sha256"
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

	// Hash
	HashConfig string

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

	// SSH
	SSHHostPort              int
	SSHBindAddress           string
	SSHAllowedClientIPs      string
	SSHAuthorizedKeysSHA256  string
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

	// Hash
	setIfNotEmpty(m, LabelHashConfig, l.HashConfig)

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

	// SSH
	if l.SSHHostPort > 0 {
		m[LabelSSHHostPort] = util.IntToString(l.SSHHostPort)
	}
	setIfNotEmpty(m, LabelSSHBindAddress, l.SSHBindAddress)
	setIfNotEmpty(m, LabelSSHAllowedClientIPs, l.SSHAllowedClientIPs)
	setIfNotEmpty(m, LabelSSHAuthorizedKeysSHA256, l.SSHAuthorizedKeysSHA256)

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

	// Hash
	l.HashConfig = m[LabelHashConfig]

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

	// SSH
	if s := m[LabelSSHHostPort]; s != "" {
		l.SSHHostPort = util.StringToInt(s)
	}
	l.SSHBindAddress = m[LabelSSHBindAddress]
	l.SSHAllowedClientIPs = m[LabelSSHAllowedClientIPs]
	l.SSHAuthorizedKeysSHA256 = m[LabelSSHAuthorizedKeysSHA256]

	return l
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
