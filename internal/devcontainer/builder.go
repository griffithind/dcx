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

	// Lockfile is the lockfile to use for feature resolution (optional)
	// When set, features will be resolved using pinned versions from the lockfile.
	Lockfile *lockfile.Lockfile

	// ForcePull forces re-fetching features from the registry
	ForcePull bool

	// ImageMetadata are the base image's devcontainer.metadata entries
	// (already merged into Config); their lifecycle hooks run first.
	ImageMetadata []DevContainerConfig

	// Raw is devcontainer.json as written (no substitution), the input of
	// the staleness key. Defaults to Local.
	Raw *DevContainerConfig

	// Local is the substituted devcontainer.json before image-metadata
	// merging; the staleness key is computed from it. Defaults to Config.
	Local *DevContainerConfig
}

// Build creates a ResolvedDevContainer from the given options.
func (b *Builder) Build(ctx context.Context, opts BuilderOptions) (*ResolvedDevContainer, error) {
	if opts.Config == nil {
		return nil, fmt.Errorf("configuration is required")
	}

	resolved := NewResolvedDevContainer()

	resolved.ConfigPath = opts.ConfigPath
	resolved.ConfigDir = filepath.Dir(opts.ConfigPath)
	resolved.LocalRoot = opts.WorkspaceRoot
	resolved.Identity = NewIdentity(opts.WorkspaceRoot, opts.Config, opts.ConfigPath)
	resolved.Config = opts.Config
	resolved.Local = opts.Local
	if resolved.Local == nil {
		resolved.Local = opts.Config
	}
	resolved.Raw = opts.Raw

	// The config has already been substituted by Load. The context is still
	// needed for values contributed by features (e.g. mounts referencing
	// ${devcontainerId}).
	subCtx := NewSubstitutionContext(opts.WorkspaceRoot, opts.ConfigPath, opts.Config)

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
		plan := NewComposePlan(composeFilePaths(opts.Config, opts.ConfigPath), opts.Config.Service, resolved.ResourceName)
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
	resolved.ForwardPorts = b.parsePorts("forwardPorts", opts.Config.ForwardPorts)

	// App ports (bound to localhost for security)
	resolved.AppPorts = b.parsePorts("appPort", opts.Config.AppPort)

	// Mounts
	resolved.Mounts = b.parseMounts(opts.Config.Mounts)

	resolved.OverrideCommand = overrideCommand(planType, opts.Config.OverrideCommand)
	resolved.HostRequirements = opts.Config.HostRequirements
	resolved.UserEnvProbe = opts.Config.UserEnvProbe

	// GPU requirements
	if opts.Config.HostRequirements != nil {
		if gpu := opts.Config.HostRequirements.GPU; gpu.Wanted() {
			resolved.GPU = gpu
		}
	}

	// dcx customizations come from devcontainer.json only (secrets run
	// commands on the host; image metadata must not supply them).
	if dcxConfig := resolved.Local.DcxCustomizations(); dcxConfig != nil {
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

	resolved.Hooks = resolveHooks(opts.Config, opts.ImageMetadata, resolved.Features, opts.Config.WaitFor)

	// Compute the staleness key from the local (pre-merge) config
	if err := b.computeHashes(resolved, resolved.Local); err != nil {
		return nil, err
	}

	// Populate build decisions
	b.populateBuildDecisions(resolved, opts.Config)

	return resolved, nil
}

// overrideCommand applies the spec's default for overrideCommand: true for
// image and Dockerfile plans, false for compose (whose service command is
// usually the point).
func overrideCommand(plan PlanType, configured *bool) bool {
	if configured != nil {
		return *configured
	}
	return plan != PlanTypeCompose
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

// featureMount applies variable substitution to a feature-declared mount
// (a string is re-parsed with the one mount parser afterwards).
func featureMount(fm Mount, subCtx *SubstitutionContext) (Mount, error) {
	if fm.Raw != "" {
		raw := Substitute(fm.Raw, subCtx)
		m, err := ParseMountString(raw)
		m.Raw = raw
		return m, err
	}
	m := fm
	m.Source = Substitute(m.Source, subCtx)
	m.Target = Substitute(m.Target, subCtx)
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

	raw := resolved.Raw
	if raw == nil {
		raw = cfg
	}
	configHash, err := ComputeConfigHash(raw, dockerfilePath, composeFiles, resolved.Features)
	if err != nil {
		return err
	}
	inputsHash, err := ComputeInputsHash(cfg, dockerfilePath, composeFiles, resolved.Features)
	if err != nil {
		return err
	}
	resolved.ConfigHash, resolved.InputsHash = configHash, inputsHash
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

// parseMounts validates config mounts (parsed when the config was read and
// again after substitution) and defaults the Type field.
func (b *Builder) parseMounts(mounts []Mount) []Mount {
	if len(mounts) == 0 {
		return nil
	}

	result := make([]Mount, 0, len(mounts))
	for _, m := range mounts {
		if m.Target == "" {
			if _, err := ParseMountString(m.Raw); err != nil {
				b.report.Warnf("Ignoring mount: %v", err)
			} else {
				b.report.Warnf("Ignoring mount without a target: %q", m.Raw)
			}
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
