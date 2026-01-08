package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/client"
	"github.com/griffithind/dcx/internal/config"
	dcxerrors "github.com/griffithind/dcx/internal/errors"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/workspace"
)

// Executor implements the Pipeline interface.
type Executor struct {
	docker         client.APIClient
	labelManager   *labels.Manager
	logger         *slog.Logger
	progress       ProgressReporter
	featureResolver workspace.FeatureResolver
	dcxVersion     string
}

// ExecutorOptions contains options for creating an executor.
type ExecutorOptions struct {
	Docker          client.APIClient
	Logger          *slog.Logger
	Progress        ProgressReporter
	FeatureResolver workspace.FeatureResolver
	DCXVersion      string
}

// NewExecutor creates a new pipeline executor.
func NewExecutor(opts ExecutorOptions) *Executor {
	if opts.Progress == nil {
		opts.Progress = NullProgressReporter{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	return &Executor{
		docker:          opts.Docker,
		labelManager:    labels.NewManager(opts.Docker, opts.Logger),
		logger:          opts.Logger,
		progress:        opts.Progress,
		featureResolver: opts.FeatureResolver,
		dcxVersion:      opts.DCXVersion,
	}
}

// Parse implements Pipeline.Parse.
func (e *Executor) Parse(ctx context.Context, opts ParseOptions) (*ParseResult, error) {
	e.progress.OnStageStart(StageParse)
	defer func() {
		e.progress.OnStageComplete(StageParse, nil)
	}()

	e.progress.OnProgress(StageProgress{Stage: StageParse, Message: "Locating configuration", Percentage: 10})

	// Discover config path if not specified
	configPath := opts.ConfigPath
	if configPath == "" {
		var err error
		configPath, err = e.discoverConfig(opts.WorkspaceRoot, opts.ConfigSearchPaths)
		if err != nil {
			return nil, err
		}
	}

	e.logger.Debug("found configuration", "path", configPath)
	e.progress.OnProgress(StageProgress{Stage: StageParse, Message: "Parsing devcontainer.json", Percentage: 30})

	// Parse configuration
	cfg, err := config.ParseFile(configPath)
	if err != nil {
		return nil, dcxerrors.ConfigParse(configPath, err)
	}

	e.progress.OnProgress(StageProgress{Stage: StageParse, Message: "Building workspace model", Percentage: 60})

	// Build initial workspace
	builder := workspace.NewBuilder(e.logger)
	ws, err := builder.Build(ctx, workspace.BuildOptions{
		ConfigPath:    configPath,
		WorkspaceRoot: opts.WorkspaceRoot,
		Config:        cfg,
		// FeatureResolver added in Resolve stage
	})
	if err != nil {
		return nil, err
	}

	// Collect additional files
	additionalFiles := []string{}
	if ws.Resolved.Dockerfile != nil {
		additionalFiles = append(additionalFiles, ws.Resolved.Dockerfile.Path)
	}
	if ws.Resolved.Compose != nil {
		additionalFiles = append(additionalFiles, ws.Resolved.Compose.Files...)
	}

	e.progress.OnProgress(StageProgress{Stage: StageParse, Message: "Parse complete", Percentage: 100})

	return &ParseResult{
		Workspace:       ws,
		ConfigPath:      configPath,
		AdditionalFiles: additionalFiles,
	}, nil
}

// Resolve implements Pipeline.Resolve.
func (e *Executor) Resolve(ctx context.Context, parsed *ParseResult) (*ResolveResult, error) {
	e.progress.OnStageStart(StageResolve)
	defer func() {
		e.progress.OnStageComplete(StageResolve, nil)
	}()

	ws := parsed.Workspace
	result := &ResolveResult{
		Workspace: ws,
	}

	// Resolve features if resolver is available and features are configured
	if e.featureResolver != nil && len(ws.RawConfig.Features) > 0 {
		e.progress.OnProgress(StageProgress{Stage: StageResolve, Message: "Resolving features", Percentage: 30})

		// Re-build workspace with feature resolver
		builder := workspace.NewBuilder(e.logger)
		resolved, err := builder.Build(ctx, workspace.BuildOptions{
			ConfigPath:      ws.ConfigPath,
			WorkspaceRoot:   ws.LocalRoot,
			Config:          ws.RawConfig,
			FeatureResolver: e.featureResolver,
		})
		if err != nil {
			return nil, err
		}

		result.Workspace = resolved
		result.FeaturesResolved = true

		// Extract feature order
		for _, f := range resolved.Resolved.Features {
			result.FeatureOrder = append(result.FeatureOrder, f.ID)
		}
	}

	e.progress.OnProgress(StageProgress{Stage: StageResolve, Message: "Resolve complete", Percentage: 100})

	return result, nil
}

// Plan implements Pipeline.Plan.
func (e *Executor) Plan(ctx context.Context, resolved *ResolveResult) (*PlanResult, error) {
	e.progress.OnStageStart(StagePlan)
	defer func() {
		e.progress.OnStageComplete(StagePlan, nil)
	}()

	ws := resolved.Workspace
	result := &PlanResult{
		Workspace: ws,
	}

	e.progress.OnProgress(StageProgress{Stage: StagePlan, Message: "Checking existing containers", Percentage: 20})

	// Find existing containers for this workspace
	existing, err := e.labelManager.FindPrimaryContainer(ctx, ws.ID)
	if err != nil {
		e.logger.Warn("error finding existing container", "error", err)
	}

	// Determine current state
	if existing == nil {
		// No existing container - need to create
		result.Action = ActionCreate
		result.Reason = "no existing container found"
		e.logger.Debug("no existing container, will create")
	} else {
		// Update workspace state from existing container
		ws.State = &workspace.RuntimeState{
			ContainerID:   existing.ID,
			ContainerName: existing.Names[0],
			Status:        containerStatusFromDockerState(existing.State),
			Labels:        existing.Labels,
		}

		e.progress.OnProgress(StageProgress{Stage: StagePlan, Message: "Checking for changes", Percentage: 50})

		// Check for staleness
		if ws.IsStale() {
			result.Action = ActionRecreate
			result.Changes = ws.GetStalenessChanges()
			result.Reason = fmt.Sprintf("configuration changed: %v", result.Changes)
			e.logger.Debug("container is stale", "changes", result.Changes)
		} else if ws.State.Status == workspace.StatusStopped {
			result.Action = ActionStart
			result.Reason = "container exists but is stopped"
		} else {
			result.Action = ActionNone
			result.Reason = "container is up to date and running"
		}
	}

	e.progress.OnProgress(StageProgress{Stage: StagePlan, Message: "Building execution plan", Percentage: 80})

	// Build plan based on action
	switch result.Action {
	case ActionCreate, ActionRecreate, ActionRebuild:
		result.ImagesToBuild = e.planImages(ws)
		result.ContainersToCreate = e.planContainers(ws)
		if ws.Resolved.Compose != nil {
			result.ServicesToStart = append([]string{ws.Resolved.Compose.Service}, ws.Resolved.Compose.RunServices...)
		}
	}

	e.progress.OnProgress(StageProgress{Stage: StagePlan, Message: "Plan complete", Percentage: 100})

	return result, nil
}

// Build implements Pipeline.Build.
func (e *Executor) Build(ctx context.Context, plan *PlanResult) (*BuildResult, error) {
	e.progress.OnStageStart(StageBuild)
	defer func() {
		e.progress.OnStageComplete(StageBuild, nil)
	}()

	result := &BuildResult{
		Workspace: plan.Workspace,
	}

	if plan.Action == ActionNone || plan.Action == ActionStart {
		// No build needed
		e.progress.OnProgress(StageProgress{Stage: StageBuild, Message: "No build needed", Percentage: 100})
		return result, nil
	}

	ws := plan.Workspace

	// Process each image build
	for i, imgPlan := range plan.ImagesToBuild {
		pct := (i * 100) / len(plan.ImagesToBuild)
		e.progress.OnProgress(StageProgress{
			Stage:      StageBuild,
			Message:    fmt.Sprintf("Building %s", imgPlan.Tag),
			Percentage: pct,
		})

		startTime := time.Now()

		// Build or pull the image
		// Note: Actual implementation would call Docker API here
		// This is a placeholder for the interface
		e.logger.Info("building image",
			"tag", imgPlan.Tag,
			"base", imgPlan.BaseImage,
			"dockerfile", imgPlan.Dockerfile)

		result.ImagesBuilt = append(result.ImagesBuilt, BuiltImage{
			Tag:       imgPlan.Tag,
			BuildTime: time.Since(startTime).Milliseconds(),
		})
	}

	// Set the derived image
	if len(result.ImagesBuilt) > 0 {
		result.DerivedImage = result.ImagesBuilt[len(result.ImagesBuilt)-1].Tag
		ws.Resolved.FinalImage = result.DerivedImage
	} else if ws.Resolved.Image != "" {
		result.DerivedImage = ws.Resolved.Image
		ws.Resolved.FinalImage = ws.Resolved.Image
	}

	e.progress.OnProgress(StageProgress{Stage: StageBuild, Message: "Build complete", Percentage: 100})

	return result, nil
}

// Deploy implements Pipeline.Deploy.
func (e *Executor) Deploy(ctx context.Context, built *BuildResult) (*DeployResult, error) {
	e.progress.OnStageStart(StageDeploy)
	defer func() {
		e.progress.OnStageComplete(StageDeploy, nil)
	}()

	result := &DeployResult{
		Workspace: built.Workspace,
	}

	ws := built.Workspace

	e.progress.OnProgress(StageProgress{Stage: StageDeploy, Message: "Creating containers", Percentage: 30})

	// Get labels for the container
	containerLabels := ws.GetBuildLabels(e.dcxVersion)

	e.logger.Info("deploying container",
		"workspace", ws.Name,
		"image", ws.Resolved.FinalImage,
		"labels", containerLabels.WorkspaceID)

	// Note: Actual implementation would create containers here
	// This is a placeholder for the interface
	result.ContainerID = "placeholder-container-id"
	result.ContainerName = ws.Resolved.ServiceName

	result.AllContainers = append(result.AllContainers, DeployedContainer{
		ID:        result.ContainerID,
		Name:      result.ContainerName,
		Service:   ws.Resolved.ServiceName,
		IsPrimary: true,
		Status:    "running",
	})

	e.progress.OnProgress(StageProgress{Stage: StageDeploy, Message: "Deploy complete", Percentage: 100})

	return result, nil
}

// Up executes the full pipeline: Parse -> Resolve -> Plan -> Build -> Deploy.
func (e *Executor) Up(ctx context.Context, opts UpOptions) (*DeployResult, error) {
	// Parse
	parsed, err := e.Parse(ctx, ParseOptions{
		ConfigPath:        opts.ConfigPath,
		WorkspaceRoot:     opts.WorkspaceRoot,
		ConfigSearchPaths: opts.ConfigSearchPaths,
	})
	if err != nil {
		return nil, err
	}

	// Resolve
	resolved, err := e.Resolve(ctx, parsed)
	if err != nil {
		return nil, err
	}

	// Plan
	plan, err := e.Plan(ctx, resolved)
	if err != nil {
		return nil, err
	}

	// Check if any action is needed
	if plan.Action == ActionNone && !opts.ForceRecreate {
		e.logger.Info("container is up to date, no action needed")
		return &DeployResult{
			Workspace:     plan.Workspace,
			ContainerID:   plan.Workspace.State.ContainerID,
			ContainerName: plan.Workspace.State.ContainerName,
		}, nil
	}

	// Override action if force flags are set
	if opts.ForceRebuild {
		plan.Action = ActionRebuild
		plan.Reason = "force rebuild requested"
	} else if opts.ForceRecreate {
		plan.Action = ActionRecreate
		plan.Reason = "force recreate requested"
	}

	// Build
	built, err := e.Build(ctx, plan)
	if err != nil {
		return nil, err
	}

	// Deploy
	return e.Deploy(ctx, built)
}

// UpOptions contains options for the Up operation.
type UpOptions struct {
	ConfigPath        string
	WorkspaceRoot     string
	ConfigSearchPaths []string
	ForceRecreate     bool
	ForceRebuild      bool
	SkipHooks         bool
}

// Helper methods

func (e *Executor) discoverConfig(workspaceRoot string, searchPaths []string) (string, error) {
	// Default search paths
	defaultPaths := []string{
		filepath.Join(workspaceRoot, ".devcontainer", "devcontainer.json"),
		filepath.Join(workspaceRoot, ".devcontainer.json"),
	}

	// Add custom search paths
	allPaths := append(defaultPaths, searchPaths...)

	for _, path := range allPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	return "", dcxerrors.ConfigNotFound(workspaceRoot)
}

func (e *Executor) planImages(ws *workspace.Workspace) []ImageBuildPlan {
	var plans []ImageBuildPlan

	switch ws.Resolved.PlanType {
	case workspace.PlanTypeImage:
		// Just need to pull base image, possibly build derived image with features
		if len(ws.Resolved.Features) > 0 {
			featureIDs := make([]string, len(ws.Resolved.Features))
			for i, f := range ws.Resolved.Features {
				featureIDs[i] = f.ID
			}
			plans = append(plans, ImageBuildPlan{
				Tag:       fmt.Sprintf("dcx-derived-%s", ws.ID[:8]),
				BaseImage: ws.Resolved.Image,
				Features:  featureIDs,
				Reason:    "feature installation",
			})
		}

	case workspace.PlanTypeDockerfile:
		// Build from Dockerfile
		plans = append(plans, ImageBuildPlan{
			Tag:        fmt.Sprintf("dcx-build-%s", ws.ID[:8]),
			Dockerfile: ws.Resolved.Dockerfile.Path,
			Context:    ws.Resolved.Dockerfile.Context,
			BuildArgs:  ws.Resolved.Dockerfile.Args,
			Reason:     "Dockerfile build",
		})
		// Add feature derivation if needed
		if len(ws.Resolved.Features) > 0 {
			featureIDs := make([]string, len(ws.Resolved.Features))
			for i, f := range ws.Resolved.Features {
				featureIDs[i] = f.ID
			}
			plans = append(plans, ImageBuildPlan{
				Tag:       fmt.Sprintf("dcx-derived-%s", ws.ID[:8]),
				BaseImage: fmt.Sprintf("dcx-build-%s", ws.ID[:8]),
				Features:  featureIDs,
				Reason:    "feature installation",
			})
		}

	case workspace.PlanTypeCompose:
		// Compose handles its own builds; we may need to add features
		if len(ws.Resolved.Features) > 0 {
			featureIDs := make([]string, len(ws.Resolved.Features))
			for i, f := range ws.Resolved.Features {
				featureIDs[i] = f.ID
			}
			plans = append(plans, ImageBuildPlan{
				Tag:      fmt.Sprintf("dcx-derived-%s", ws.ID[:8]),
				Features: featureIDs,
				Reason:   "feature installation for compose service",
			})
		}
	}

	return plans
}

func (e *Executor) planContainers(ws *workspace.Workspace) []ContainerPlan {
	var plans []ContainerPlan

	// Primary container
	plans = append(plans, ContainerPlan{
		Name:      ws.Resolved.ServiceName,
		Image:     ws.Resolved.FinalImage,
		Service:   ws.Resolved.ServiceName,
		IsPrimary: true,
	})

	return plans
}

func containerStatusFromDockerState(state string) workspace.ContainerStatus {
	switch state {
	case "running":
		return workspace.StatusRunning
	case "created":
		return workspace.StatusCreated
	case "exited", "dead":
		return workspace.StatusStopped
	default:
		return workspace.StatusAbsent
	}
}
