package devcontainer

import (
	"context"
	"fmt"
	"github.com/griffithind/dcx/internal/report"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lockfile"
	"gopkg.in/yaml.v3"
)

// Builder constructs a ResolvedDevContainer from configuration and resolves all references.
type Builder struct {
	logger *slog.Logger
	report report.Reporter
}

// NewBuilder creates a new devcontainer builder. Progress from feature
// resolution is sent to rep (nil discards it).
func NewBuilder(logger *slog.Logger, rep report.Reporter) *Builder {
	return &Builder{logger: logger, report: report.OrDiscard(rep)}
}

// BuilderOptions contains options for building a resolved devcontainer.
type BuilderOptions struct {
	// ConfigPath is the path to devcontainer.json
	ConfigPath string

	// WorkspaceRoot is the workspace root directory
	WorkspaceRoot string

	// Config is the parsed devcontainer configuration
	Config *DevContainerConfig

	// SubstitutionContext provides variable substitution values
	SubstitutionContext *SubstitutionContext

	// ProjectName is the sanitized project name from devcontainer.json name field
	ProjectName string

	// Lockfile is the lockfile to use for feature resolution (optional)
	// When set, features will be resolved using pinned versions from the lockfile.
	Lockfile *lockfile.Lockfile

	// ForcePull forces re-fetching features from the registry
	ForcePull bool

	// LocalConfig is the substituted devcontainer.json before image-metadata
	// merging; the staleness key is computed from it. Defaults to Config.
	LocalConfig *DevContainerConfig
}

// Build creates a ResolvedDevContainer from the given options.
func (b *Builder) Build(ctx context.Context, opts BuilderOptions) (*ResolvedDevContainer, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("configuration is required")
	}

	resolved := NewResolvedDevContainer()

	// Set identity
	resolved.ConfigPath = opts.ConfigPath
	resolved.ConfigDir = filepath.Dir(opts.ConfigPath)
	resolved.LocalRoot = opts.WorkspaceRoot
	resolved.ID = ComputeID(opts.WorkspaceRoot)

	// Use project name if provided, otherwise compute from config
	if opts.ProjectName != "" {
		resolved.Name = opts.ProjectName
	} else {
		resolved.Name = ComputeName(opts.WorkspaceRoot, opts.Config)
	}
	resolved.RawConfig = opts.Config

	// The config has already been substituted by Load. The context is still
	// needed for values contributed by features (e.g. mounts referencing
	// ${devcontainerId}).
	subCtx := opts.SubstitutionContext
	if subCtx == nil {
		subCtx = NewSubstitutionContext(opts.WorkspaceRoot, opts.Config)
	}

	// Create execution plan based on config type
	planType := opts.Config.PlanType()
	switch planType {
	case PlanTypeImage:
		resolved.Plan = NewImagePlan(opts.Config.Image)
		resolved.BaseImage = opts.Config.Image

	case PlanTypeDockerfile:
		dockerfilePath := filepath.Join(resolved.ConfigDir, opts.Config.Build.Dockerfile)
		contextPath := resolved.ConfigDir
		if opts.Config.Build.Context != "" {
			contextPath = filepath.Join(resolved.ConfigDir, opts.Config.Build.Context)
		}
		plan := NewDockerfilePlan(dockerfilePath, contextPath)
		plan.Args = opts.Config.Build.Args
		plan.Target = opts.Config.Build.Target
		plan.CacheFrom = opts.Config.Build.CacheFrom
		plan.Options = opts.Config.Build.Options
		resolved.Plan = plan

	case PlanTypeCompose:
		composeFiles := opts.Config.DockerComposeFile
		absolutePaths := make([]string, len(composeFiles))
		for i, f := range composeFiles {
			absolutePaths[i] = filepath.Join(resolved.ConfigDir, f)
		}

		// For compose projects, prefer explicit compose.yaml name over devcontainer.json name
		// Only use compose name if explicitly set in compose.yaml (not auto-derived from directory)
		projectName := ""
		if explicitName := getExplicitProjectName(absolutePaths); explicitName != "" {
			// Use explicit compose.yaml name
			projectName = SanitizeProjectName(explicitName)
			// Also update resolved.Name so SSH host and identifiers use compose name
			resolved.Name = explicitName
		}
		if projectName == "" {
			projectName = ResourceName(resolved.Name, resolved.ID)
		}

		plan := NewComposePlan(absolutePaths, opts.Config.Service, projectName)
		plan.RunServices = opts.Config.RunServices
		resolved.Plan = plan
	}

	// Resolve workspace paths
	resolved.WorkspaceFolder = DetermineContainerWorkspaceFolder(opts.Config, opts.WorkspaceRoot)
	resolved.WorkspaceMount = opts.Config.WorkspaceMount

	// User configuration
	resolved.RemoteUser = opts.Config.RemoteUser
	resolved.ContainerUser = opts.Config.ContainerUser
	if opts.Config.UpdateRemoteUserUID != nil {
		resolved.UpdateRemoteUserUID = *opts.Config.UpdateRemoteUserUID
	}

	// Effective user
	resolved.EffectiveUser = resolved.RemoteUser
	if resolved.EffectiveUser == "" {
		resolved.EffectiveUser = resolved.ContainerUser
	}
	resolved.HostUID = os.Getuid()
	resolved.HostGID = os.Getgid()

	// Environment variables
	for k, v := range opts.Config.ContainerEnv {
		resolved.ContainerEnv[k] = v
	}
	for k, v := range opts.Config.RemoteEnv {
		resolved.RemoteEnv[k] = v
	}

	// Runtime options
	resolved.CapAdd = opts.Config.CapAdd
	resolved.SecurityOpt = opts.Config.SecurityOpt
	if opts.Config.Privileged != nil {
		resolved.Privileged = *opts.Config.Privileged
	}
	if opts.Config.Init != nil {
		resolved.Init = *opts.Config.Init
	}

	// Parse runArgs into structured form
	if len(opts.Config.RunArgs) > 0 {
		resolved.RunArgs = ParseRunArgs(opts.Config.RunArgs)
		for _, arg := range resolved.RunArgs.Ignored {
			b.report.Warnf("runArgs: ignoring %s (dcx manages the container's name and lifetime)", arg)
		}
		if planType == PlanTypeCompose {
			b.report.Warnf("runArgs are not applied to Docker Compose configurations; set these options in the compose file")
		}
	}

	// Forward ports
	resolved.ForwardPorts = b.parsePorts("forwardPorts", []interface{}(opts.Config.ForwardPorts))

	// App ports (bound to localhost for security)
	resolved.AppPorts = b.parsePorts("appPort", opts.Config.AppPort)

	// Mounts
	resolved.Mounts = b.parseMounts(opts.Config.Mounts)

	// Container name: unique per workspace (see ResourceName)
	resolved.ServiceName = ResourceName(resolved.Name, resolved.ID)

	// Customizations
	resolved.Customizations = opts.Config.Customizations

	// GPU requirements
	if opts.Config.HostRequirements != nil {
		resolved.GPURequirements = parseGPURequirements(opts.Config.HostRequirements)
	}

	// Extract secrets from DCX customizations
	if dcxConfig := opts.Config.DcxCustomizations(); dcxConfig != nil {
		if len(dcxConfig.Secrets) > 0 {
			resolved.RuntimeSecrets = dcxConfig.Secrets
		}
		if len(dcxConfig.BuildSecrets) > 0 {
			resolved.BuildSecrets = dcxConfig.BuildSecrets
		}
	}

	// Resolve features if any exist
	if len(opts.Config.Features) > 0 {
		if err := b.resolveFeatures(ctx, resolved, opts, subCtx); err != nil {
			return nil, err
		}
	}

	// Compute the staleness key from the local (pre-merge) config
	keyConfig := opts.LocalConfig
	if keyConfig == nil {
		keyConfig = opts.Config
	}
	resolved.LocalConfig = keyConfig
	if err := b.computeHashes(resolved, keyConfig); err != nil {
		return nil, err
	}

	// Populate build decisions
	b.populateBuildDecisions(resolved, opts.Config)

	// Initialize labels

	return resolved, nil
}

// resolveFeatures resolves all features from the configuration.
func (b *Builder) resolveFeatures(ctx context.Context, resolved *ResolvedDevContainer, opts BuilderOptions, subCtx *SubstitutionContext) error {
	mgr, err := features.NewManager(resolved.ConfigDir, b.report)
	if err != nil {
		return fmt.Errorf("failed to create feature manager: %w", err)
	}

	// Configure manager with lockfile and force pull settings
	if opts.Lockfile != nil {
		mgr.SetLockfile(opts.Lockfile)
	}
	if opts.ForcePull {
		mgr.SetForcePull(true)
	}

	feats, err := mgr.ResolveAll(ctx, opts.Config.Features, opts.Config.OverrideFeatureInstallOrder)
	if err != nil {
		return fmt.Errorf("failed to resolve features: %w", err)
	}

	resolved.Features = feats

	// Merge feature mounts, capAdd, securityOpt, etc.
	b.mergeFeatureRuntimeConfig(resolved, feats, subCtx)

	return nil
}

// mergeFeatureRuntimeConfig merges runtime configuration from features into resolved.
//
// Feature containerEnv is not merged here: it is baked into the derived
// image as ENV (where ${VAR} references expand against the image), and
// passing it again at runtime would override user containerEnv and freeze
// unexpanded references.
func (b *Builder) mergeFeatureRuntimeConfig(resolved *ResolvedDevContainer, feats []*features.Feature, subCtx *SubstitutionContext) {
	for _, feat := range feats {
		if feat.Metadata == nil {
			continue
		}

		for _, fm := range feat.Metadata.Mounts {
			m, err := featureMount(fm, subCtx)
			if err != nil {
				b.report.Warnf("Ignoring mount from feature %s: %v", feat.ID, err)
				continue
			}
			resolved.Mounts = mergeMountByTarget(resolved.Mounts, m)
		}

		for _, c := range feat.Metadata.CapAdd {
			if !contains(resolved.CapAdd, c) {
				resolved.CapAdd = append(resolved.CapAdd, c)
			}
		}
		for _, opt := range feat.Metadata.SecurityOpt {
			if !contains(resolved.SecurityOpt, opt) {
				resolved.SecurityOpt = append(resolved.SecurityOpt, opt)
			}
		}
		if feat.Metadata.Privileged {
			resolved.Privileged = true
		}
		if feat.Metadata.Init {
			resolved.Init = true
		}
	}
}

// featureMount converts a feature-declared mount, applying the same variable
// substitution and parser as devcontainer.json mounts.
func featureMount(fm features.FeatureMount, subCtx *SubstitutionContext) (Mount, error) {
	if fm.Raw != "" {
		return ParseMountString(Substitute(fm.Raw, subCtx))
	}
	m := Mount{
		Source: Substitute(fm.Source, subCtx),
		Target: Substitute(fm.Target, subCtx),
		Type:   fm.Type,
	}
	if m.Type == "" {
		m.Type = "bind"
	}
	if m.Target == "" {
		return Mount{}, fmt.Errorf("missing target")
	}
	return m, nil
}

// mergeMountByTarget adds m, replacing an existing mount with the same
// target (a container path can only be mounted once).
func mergeMountByTarget(mounts []Mount, m Mount) []Mount {
	for i := range mounts {
		if mounts[i].Target == m.Target {
			mounts[i] = m
			return mounts
		}
	}
	return append(mounts, m)
}

// contains checks if a string slice contains a value.
func contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// computeHashes computes the configuration hash from all build inputs.
func (b *Builder) computeHashes(resolved *ResolvedDevContainer, cfg *DevContainerConfig) error {
	var dockerfilePath string
	var composeFiles []string

	if df, ok := resolved.Plan.(*DockerfilePlan); ok {
		dockerfilePath = df.Dockerfile
	}
	if cp, ok := resolved.Plan.(*ComposePlan); ok {
		composeFiles = cp.Files
	}

	configHash, err := ComputeConfigHash(cfg, dockerfilePath, composeFiles, resolved.Features)
	if err != nil {
		return err
	}

	resolved.ConfigHash = configHash
	return nil
}

// populateBuildDecisions populates build-time decisions.
func (b *Builder) populateBuildDecisions(resolved *ResolvedDevContainer, cfg *DevContainerConfig) {
	// UID update decision
	if resolved.EffectiveUser != "" && resolved.EffectiveUser != "root" && resolved.HostUID != 0 {
		shouldUpdate := true
		if cfg.UpdateRemoteUserUID != nil {
			shouldUpdate = *cfg.UpdateRemoteUserUID
		}
		resolved.ShouldUpdateUID = shouldUpdate
	}
}

// parseGPURequirements parses GPU requirements from host requirements.
func parseGPURequirements(hr *HostRequirements) *GPURequirements {
	if hr == nil || hr.GPU == nil {
		return nil
	}

	gpu := &GPURequirements{}

	switch v := hr.GPU.(type) {
	case bool:
		gpu.Enabled = v
	case string:
		gpu.Enabled = v == "true" || v == "optional"
		gpu.Optional = v == "optional"
	case map[string]interface{}:
		// An empty object states no requirement (as in hostreqs.go).
		gpu.Enabled = len(v) > 0
		if count, ok := v["count"].(float64); ok {
			gpu.Count = int(count)
		}
		if memory, ok := v["memory"].(string); ok {
			gpu.Memory = memory
		}
		if cores, ok := v["cores"].(float64); ok {
			gpu.Cores = int(cores)
		}
	}

	return gpu
}

// parseMounts validates config mounts and defaults the Type field.
func (b *Builder) parseMounts(mounts []Mount) []Mount {
	if len(mounts) == 0 {
		return nil
	}

	result := make([]Mount, 0, len(mounts))
	for _, m := range mounts {
		if m.Target == "" && m.Raw != "" {
			parsed, err := ParseMountString(m.Raw)
			if err != nil {
				b.report.Warnf("Ignoring mount: %v", err)
				continue
			}
			parsed.Raw = m.Raw
			m = parsed
		}
		if m.Target == "" {
			b.report.Warnf("Ignoring mount without a target: %q", m.Raw)
			continue
		}
		if m.Type == "" {
			m.Type = "bind"
		}
		result = mergeMountByTarget(result, m)
	}
	return result
}

// getExplicitProjectName checks compose files for an explicit "name" field.
// Returns the last name found (matching Docker Compose merge behavior).
func getExplicitProjectName(files []string) string {
	var name string
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var raw map[string]interface{}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			continue
		}

		if n, ok := raw["name"].(string); ok && n != "" {
			name = n // Keep going, use the last one
		}
	}
	return name
}
