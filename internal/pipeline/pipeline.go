// Package pipeline provides a staged execution pipeline for devcontainer operations.
// The pipeline consists of five stages: Parse, Resolve, Plan, Build, Deploy.
package pipeline

import (
	"context"

	"github.com/griffithind/dcx/internal/workspace"
)

// Stage represents a pipeline stage.
type Stage string

const (
	StageParse   Stage = "parse"
	StageResolve Stage = "resolve"
	StagePlan    Stage = "plan"
	StageBuild   Stage = "build"
	StageDeploy  Stage = "deploy"
)

// Pipeline defines the interface for executing devcontainer operations.
type Pipeline interface {
	// Parse reads and parses devcontainer.json and related configuration files.
	Parse(ctx context.Context, opts ParseOptions) (*ParseResult, error)

	// Resolve performs variable substitution, feature resolution, and metadata merging.
	Resolve(ctx context.Context, parsed *ParseResult) (*ResolveResult, error)

	// Plan determines what needs to be built/deployed based on current state.
	Plan(ctx context.Context, resolved *ResolveResult) (*PlanResult, error)

	// Build creates images as needed (base image, feature derivation).
	Build(ctx context.Context, plan *PlanResult) (*BuildResult, error)

	// Deploy creates/starts containers based on the build result.
	Deploy(ctx context.Context, built *BuildResult) (*DeployResult, error)
}

// ParseOptions contains options for the parse stage.
type ParseOptions struct {
	// ConfigPath is the path to devcontainer.json (optional, will be discovered if not set).
	ConfigPath string

	// WorkspaceRoot is the workspace root directory.
	WorkspaceRoot string

	// ConfigSearchPaths are additional paths to search for devcontainer.json.
	ConfigSearchPaths []string
}

// ParseResult contains the output of the parse stage.
type ParseResult struct {
	// Workspace is the partially built workspace (before resolution).
	Workspace *workspace.Workspace

	// ConfigPath is the resolved path to devcontainer.json.
	ConfigPath string

	// AdditionalFiles contains paths to related files (Dockerfile, compose files).
	AdditionalFiles []string

	// Warnings contains non-fatal issues encountered during parsing.
	Warnings []string
}

// ResolveResult contains the output of the resolve stage.
type ResolveResult struct {
	// Workspace is the fully resolved workspace.
	Workspace *workspace.Workspace

	// FeaturesResolved indicates whether features were resolved.
	FeaturesResolved bool

	// FeatureOrder is the ordered list of feature IDs.
	FeatureOrder []string

	// Warnings contains non-fatal issues encountered during resolution.
	Warnings []string
}

// PlanResult contains the output of the plan stage.
type PlanResult struct {
	// Workspace is the workspace with build plan attached.
	Workspace *workspace.Workspace

	// Action indicates what action is needed.
	Action PlanAction

	// Reason explains why the action is needed.
	Reason string

	// Changes lists specific changes detected.
	Changes []string

	// ImagesToBuild lists images that need to be built.
	ImagesToBuild []ImageBuildPlan

	// ContainersToCreate lists containers to be created.
	ContainersToCreate []ContainerPlan

	// ServicesToStart lists services to start (for compose).
	ServicesToStart []string
}

// PlanAction indicates the type of action needed.
type PlanAction string

const (
	ActionNone      PlanAction = "none"      // Already up to date
	ActionCreate    PlanAction = "create"    // Create new containers
	ActionRecreate  PlanAction = "recreate"  // Recreate due to changes
	ActionStart     PlanAction = "start"     // Start existing containers
	ActionRebuild   PlanAction = "rebuild"   // Rebuild images and recreate
)

// ImageBuildPlan describes an image that needs to be built.
type ImageBuildPlan struct {
	Tag        string            // Target image tag
	BaseImage  string            // Base image to build from
	Dockerfile string            // Dockerfile path (if applicable)
	Context    string            // Build context path
	BuildArgs  map[string]string // Build arguments
	Features   []string          // Features to install
	Reason     string            // Why this build is needed
}

// ContainerPlan describes a container to be created.
type ContainerPlan struct {
	Name       string            // Container name
	Image      string            // Image to use
	Service    string            // Service name (for compose)
	IsPrimary  bool              // Is this the primary devcontainer
	Labels     map[string]string // Labels to apply
}

// BuildResult contains the output of the build stage.
type BuildResult struct {
	// Workspace is the workspace after builds complete.
	Workspace *workspace.Workspace

	// ImagesBuilt lists images that were built.
	ImagesBuilt []BuiltImage

	// BaseImagePulled indicates if the base image was pulled.
	BaseImagePulled bool

	// DerivedImage is the final image after feature derivation.
	DerivedImage string
}

// BuiltImage describes a built image.
type BuiltImage struct {
	Tag       string
	Digest    string
	Size      int64
	BuildTime int64 // milliseconds
}

// DeployResult contains the output of the deploy stage.
type DeployResult struct {
	// Workspace is the workspace after deployment.
	Workspace *workspace.Workspace

	// ContainerID is the primary container ID.
	ContainerID string

	// ContainerName is the primary container name.
	ContainerName string

	// AllContainers lists all created/started containers.
	AllContainers []DeployedContainer

	// LifecycleHooksRun indicates which hooks were executed.
	LifecycleHooksRun []string
}

// DeployedContainer describes a deployed container.
type DeployedContainer struct {
	ID        string
	Name      string
	Service   string
	IsPrimary bool
	Status    string
}

// StageProgress reports progress for a pipeline stage.
type StageProgress struct {
	Stage      Stage
	Message    string
	Percentage int // 0-100, or -1 for indeterminate
}

// ProgressReporter receives progress updates during pipeline execution.
type ProgressReporter interface {
	OnProgress(progress StageProgress)
	OnStageStart(stage Stage)
	OnStageComplete(stage Stage, err error)
}

// NullProgressReporter is a no-op progress reporter.
type NullProgressReporter struct{}

func (NullProgressReporter) OnProgress(StageProgress)          {}
func (NullProgressReporter) OnStageStart(Stage)               {}
func (NullProgressReporter) OnStageComplete(Stage, error)     {}
