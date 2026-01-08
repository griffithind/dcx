// Package workspace provides a unified in-memory model for devcontainer workspaces.
// It represents the fully resolved state of a workspace including configuration,
// features, build artifacts, and runtime state.
package workspace

import (
	"crypto/sha256"
	"encoding/base32"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/util"
)

// Workspace represents a fully resolved devcontainer workspace.
type Workspace struct {
	// Identity
	ID         string // Stable identifier (hash of workspace path)
	Name       string // Human-readable name (from config or derived)
	ConfigPath string // Absolute path to devcontainer.json
	ConfigDir  string // Directory containing devcontainer.json
	LocalRoot  string // Workspace root on host

	// Source configuration (as-parsed)
	RawConfig *config.DevcontainerConfig

	// Resolved configuration (after all processing)
	Resolved *ResolvedConfig

	// Build artifacts
	Build *BuildPlan

	// Runtime state (from container labels if available)
	State *RuntimeState

	// Computed hashes for staleness detection
	Hashes *HashSet

	// Labels for container operations
	Labels *labels.Labels
}

// ResolvedConfig represents fully resolved configuration after variable
// substitution, feature resolution, and metadata merging.
type ResolvedConfig struct {
	// Plan type
	PlanType PlanType

	// Image configuration (final image to use or build from)
	Image       string
	FinalImage  string // After feature derivation
	ServiceName string // Container/service name

	// Dockerfile build (if applicable)
	Dockerfile *DockerfilePlan

	// Compose configuration (if applicable)
	Compose *ComposePlan

	// Features (ordered by installation order)
	Features []*ResolvedFeature

	// Workspace paths
	WorkspaceFolder          string // Inside container
	WorkspaceMount           string // Mount specification

	// User configuration
	RemoteUser          string
	ContainerUser       string
	UpdateRemoteUserUID bool

	// Environment variables
	ContainerEnv map[string]string // Set at container creation
	RemoteEnv    map[string]string // Set in shell sessions

	// Runtime options
	Mounts      []mount.Mount
	CapAdd      []string
	SecurityOpt []string
	Privileged  bool
	Init        bool
	RunArgs     []string

	// Ports
	ForwardPorts []PortForward
	AppPorts     []PortForward

	// Lifecycle hooks
	Hooks *LifecycleHooks

	// Customizations (deep-merged)
	Customizations map[string]interface{}

	// Host requirements
	GPURequirements *GPURequirements
}

// PlanType identifies the execution plan type.
type PlanType string

const (
	PlanTypeImage      PlanType = "image"
	PlanTypeDockerfile PlanType = "dockerfile"
	PlanTypeCompose    PlanType = "compose"
)

// DockerfilePlan contains Dockerfile build configuration.
type DockerfilePlan struct {
	Path       string            // Path to Dockerfile
	Context    string            // Build context path
	Args       map[string]string // Build arguments
	Target     string            // Target stage
	CacheFrom  []string          // Cache sources
	Options    []string          // Additional build options
	BaseImage  string            // Extracted base image from Dockerfile
}

// ComposePlan contains docker-compose configuration.
type ComposePlan struct {
	Files          []string // Paths to compose files
	Service        string   // Primary service name
	RunServices    []string // Additional services to start
	ProjectName    string   // Compose project name
	WorkDir        string   // Working directory for compose commands
}

// ResolvedFeature represents a resolved and validated feature.
type ResolvedFeature struct {
	ID            string                            // Feature identifier (e.g., "ghcr.io/devcontainers/features/go:1")
	LocalPath     string                            // Path to extracted feature
	Options       map[string]interface{}            // Feature options
	Metadata      *FeatureMetadata                  // Parsed devcontainer-feature.json
	Digest        string                            // OCI digest (for registry features)
	InstallOrder  int                               // Order in installation sequence
}

// FeatureMetadata contains feature metadata from devcontainer-feature.json.
type FeatureMetadata struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Version      string                 `json:"version"`
	Description  string                 `json:"description"`
	Options      map[string]interface{} `json:"options"`
	DependsOn    map[string]interface{} `json:"dependsOn"`
	InstallsAfter []string              `json:"installsAfter"`
	Entrypoint   string                 `json:"entrypoint"`
	CapAdd       []string               `json:"capAdd"`
	SecurityOpt  []string               `json:"securityOpt"`
	Privileged   bool                   `json:"privileged"`
	Init         bool                   `json:"init"`
	ContainerEnv map[string]string      `json:"containerEnv"`
	Mounts       []config.Mount         `json:"mounts"`
	Customizations map[string]interface{} `json:"customizations"`
}

// PortForward represents a port forwarding configuration.
type PortForward struct {
	HostPort      int
	ContainerPort int
	Label         string
	Protocol      string
	OnAutoForward string
}

// LifecycleHooks contains all lifecycle hook commands.
type LifecycleHooks struct {
	Initialize    []HookCommand
	OnCreate      []HookCommand
	UpdateContent []HookCommand
	PostCreate    []HookCommand
	PostStart     []HookCommand
	PostAttach    []HookCommand
	WaitFor       string // Which hook to wait for before considering ready

	// Feature hooks (run before devcontainer hooks)
	FeatureOnCreate      []HookCommand
	FeatureUpdateContent []HookCommand
	FeaturePostCreate    []HookCommand
	FeaturePostStart     []HookCommand
	FeaturePostAttach    []HookCommand
}

// HookCommand represents a single lifecycle hook command.
type HookCommand struct {
	Name     string   // Command name (for map-style commands)
	Command  string   // Shell command (if string format)
	Args     []string // Command args (if array format)
	Parallel bool     // Run in parallel with other named commands
}

// GPURequirements specifies GPU requirements.
type GPURequirements struct {
	Enabled bool
	Count   int
	Memory  string
	Cores   int
}

// BuildPlan represents what needs to be built.
type BuildPlan struct {
	NeedsBuild     bool   // Whether any build is required
	BuildReason    string // Why build is needed (new, stale, forced)
	ImageToBuild   string // Image name to build
	DerivedImage   string // Derived image with features
	BaseImage      string // Original base image
	BuildTimestamp time.Time
}

// RuntimeState represents container runtime state.
type RuntimeState struct {
	ContainerID    string
	ContainerName  string
	Status         ContainerStatus
	CreatedAt      time.Time
	StartedAt      time.Time
	LifecycleState string // created, ready, broken
	Labels         *labels.Labels
}

// ContainerStatus represents container state.
type ContainerStatus string

const (
	StatusAbsent   ContainerStatus = "absent"
	StatusCreated  ContainerStatus = "created"
	StatusRunning  ContainerStatus = "running"
	StatusStopped  ContainerStatus = "stopped"
	StatusStale    ContainerStatus = "stale"
	StatusBroken   ContainerStatus = "broken"
)

// HashSet contains hashes for staleness detection.
type HashSet struct {
	Config     string // devcontainer.json canonical hash
	Dockerfile string // Dockerfile content hash
	Compose    string // docker-compose.yml hash
	Features   string // Combined features hash
	Overall    string // Combined hash of all above
}

// New creates a new empty Workspace.
func New() *Workspace {
	return &Workspace{
		Resolved: &ResolvedConfig{
			ContainerEnv:   make(map[string]string),
			RemoteEnv:      make(map[string]string),
			Customizations: make(map[string]interface{}),
		},
		Hashes: &HashSet{},
		State:  &RuntimeState{},
		Labels: labels.NewLabels(),
	}
}

// ComputeID generates a stable workspace identifier from the workspace path.
// Returns base32(sha256(realpath(workspace_root)))[0:12].
// This is the canonical identifier used for container labels, compose projects,
// SSH hosts, and all workspace lookups.
func ComputeID(workspacePath string) string {
	// Get the real path (resolve symlinks)
	realPath, err := util.RealPath(workspacePath)
	if err != nil {
		// Fall back to the original path if we can't resolve
		realPath = workspacePath
	}

	// Normalize the path
	realPath = util.NormalizePath(realPath)

	// Compute SHA256
	hash := sha256.Sum256([]byte(realPath))

	// Encode as base32 and take first 12 characters
	encoded := base32.StdEncoding.EncodeToString(hash[:])
	encoded = strings.ToLower(encoded)

	if len(encoded) > 12 {
		encoded = encoded[:12]
	}

	return encoded
}

// ComputeFullHash computes the full base32-encoded hash of the workspace path.
// This is used for the workspace_root_hash label.
func ComputeFullHash(workspacePath string) string {
	// Get the real path (resolve symlinks)
	realPath, err := util.RealPath(workspacePath)
	if err != nil {
		realPath = workspacePath
	}
	realPath = util.NormalizePath(realPath)

	hash := sha256.Sum256([]byte(realPath))
	return base32.StdEncoding.EncodeToString(hash[:])
}

// ComputeName derives a workspace name from the path or config.
func ComputeName(workspacePath string, cfg *config.DevcontainerConfig) string {
	if cfg != nil && cfg.Name != "" {
		return cfg.Name
	}
	return filepath.Base(workspacePath)
}

// GetPlanType determines the plan type from configuration.
func GetPlanType(cfg *config.DevcontainerConfig) PlanType {
	if cfg.IsComposePlan() {
		return PlanTypeCompose
	}
	if cfg.Build != nil {
		return PlanTypeDockerfile
	}
	return PlanTypeImage
}

// IsStale checks if the workspace configuration is stale compared to container state.
func (w *Workspace) IsStale() bool {
	if w.State == nil || w.State.Labels == nil {
		return true
	}
	if w.State.Labels.HashOverall == "" {
		return true
	}
	return w.State.Labels.HashOverall != w.Hashes.Overall
}

// GetStalenessChanges returns what changed if workspace is stale.
func (w *Workspace) GetStalenessChanges() []string {
	if w.State == nil || w.State.Labels == nil {
		return []string{"container not found"}
	}

	var changes []string
	l := w.State.Labels

	if l.HashConfig != "" && l.HashConfig != w.Hashes.Config {
		changes = append(changes, "devcontainer.json changed")
	}
	if l.HashDockerfile != "" && l.HashDockerfile != w.Hashes.Dockerfile {
		changes = append(changes, "Dockerfile changed")
	}
	if l.HashCompose != "" && l.HashCompose != w.Hashes.Compose {
		changes = append(changes, "docker-compose.yml changed")
	}
	if l.HashFeatures != "" && l.HashFeatures != w.Hashes.Features {
		changes = append(changes, "features changed")
	}

	if len(changes) == 0 && w.IsStale() {
		changes = append(changes, "configuration changed")
	}

	return changes
}

// NeedsRebuild determines if the workspace needs to be rebuilt.
func (w *Workspace) NeedsRebuild() bool {
	if w.State == nil || w.State.Status == StatusAbsent {
		return true
	}
	return w.IsStale()
}

// GetBuildLabels returns labels to apply during container creation.
func (w *Workspace) GetBuildLabels(dcxVersion string) *labels.Labels {
	l := labels.NewLabels()

	// Identity
	l.WorkspaceID = w.ID
	l.WorkspaceName = w.Name
	l.WorkspacePath = w.LocalRoot
	l.ConfigPath = w.ConfigPath

	// Hashes
	l.HashConfig = w.Hashes.Config
	l.HashDockerfile = w.Hashes.Dockerfile
	l.HashCompose = w.Hashes.Compose
	l.HashFeatures = w.Hashes.Features
	l.HashOverall = w.Hashes.Overall

	// State
	l.CreatedAt = time.Now()
	l.CreatedBy = dcxVersion
	l.LifecycleState = labels.LifecycleStateCreated

	// Build info
	if w.Resolved != nil {
		l.BuildMethod = string(w.Resolved.PlanType)
		l.BaseImage = w.Resolved.Image
		l.DerivedImage = w.Resolved.FinalImage

		// Features
		if len(w.Resolved.Features) > 0 {
			featureIDs := make([]string, len(w.Resolved.Features))
			featureConfig := make(map[string]map[string]interface{})
			for i, f := range w.Resolved.Features {
				featureIDs[i] = f.ID
				if len(f.Options) > 0 {
					featureConfig[f.ID] = f.Options
				}
			}
			l.FeaturesInstalled = featureIDs
			l.FeaturesConfig = featureConfig
		}

		// Compose
		if w.Resolved.Compose != nil {
			l.ComposeProject = w.Resolved.Compose.ProjectName
			l.ComposeService = w.Resolved.Compose.Service
		}
	}

	// Cache
	l.CacheData = &labels.CacheData{
		ConfigHash:     w.Hashes.Config,
		DockerfileHash: w.Hashes.Dockerfile,
		ComposeHash:    w.Hashes.Compose,
		FeaturesHash:   w.Hashes.Features,
		LastChecked:    time.Now(),
	}

	return l
}
