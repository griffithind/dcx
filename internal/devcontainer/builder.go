package devcontainer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lockfile"
	"github.com/griffithind/dcx/internal/report"
	"gopkg.in/yaml.v3"
)

// Builder constructs a ResolvedDevContainer from configuration and resolves all references.
type Builder struct {
	report report.Reporter
}

// NewBuilder creates a new devcontainer builder. Progress from feature
// resolution is sent to rep (nil discards it).
func NewBuilder(rep report.Reporter) *Builder {
	return &Builder{report: report.OrDiscard(rep)}
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

	// MetadataImage is the image ImageMetadata was read from, and
	// MetadataLabel its raw label.
	MetadataImage string
	MetadataLabel string

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

	planType := opts.Config.PlanType()
	resolved.Plan = buildPlan(opts.Config, opts.ConfigPath, resolved.ResourceName)
	if planType == PlanTypeImage {
		resolved.BaseImage = opts.Config.Image
	}
	resolved.WorkspaceFolder = DetermineContainerWorkspaceFolder(opts.Config, opts.WorkspaceRoot)
	resolved.WorkspaceMount = resolveWorkspaceMount(opts.Config, opts.WorkspaceRoot)
	resolveUsers(resolved, opts.Config)
	b.resolveRuntime(resolved, opts.Config, opts.WorkspaceRoot)

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

	resolved.Hooks = resolveHooks(opts.Config, opts.ImageMetadata, resolved.Features)
	resolved.Entrypoints = resolveEntrypoints(opts.ImageMetadata, resolved.Features)
	resolved.MetadataImage, resolved.MetadataLabel = opts.MetadataImage, opts.MetadataLabel

	// Compute the staleness key from the local (pre-merge) config
	if err := b.computeHashes(resolved, resolved.Local); err != nil {
		return nil, err
	}

	// Populate build decisions
	b.populateBuildDecisions(resolved, opts.Config)

	return resolved, nil
}

// resolveEntrypoints lists the entrypoint scripts that run when the
// container starts: those recorded in the base image's metadata (features
// a prebuilt image installed, e.g. docker-in-docker's dockerd init), then
// those of the features dcx installs, each once.
func resolveEntrypoints(imageConfigs []DevContainerConfig, feats []*features.Feature) []string {
	var out []string
	add := func(ep string) {
		if ep != "" && !slices.Contains(out, ep) {
			out = append(out, ep)
		}
	}
	for _, c := range imageConfigs {
		add(c.Entrypoint)
	}
	for _, f := range feats {
		if f.Metadata != nil {
			add(f.Metadata.Entrypoint)
		}
	}
	return out
}

// buildPlan creates the execution plan of the config. A build without a
// dockerfile uses <context>/Dockerfile (Docker's default).
func buildPlan(cfg *DevContainerConfig, configPath, resourceName string) ExecutionPlan {
	configDir := filepath.Dir(configPath)
	switch cfg.PlanType() {
	case PlanTypeDockerfile:
		// Paths are relative to the config file's directory (absolute ones
		// are kept). Without a context, the context is the Dockerfile's
		// directory; without a dockerfile (a dcx extension), the
		// Dockerfile is <context>/Dockerfile.
		fromConfig := func(p string) string {
			if filepath.IsAbs(p) {
				return filepath.Clean(p)
			}
			return filepath.Join(configDir, p)
		}
		var contextPath, dockerfilePath string
		switch {
		case cfg.Build.Dockerfile != "":
			dockerfilePath = fromConfig(cfg.Build.Dockerfile)
			contextPath = filepath.Dir(dockerfilePath)
			if cfg.Build.Context != "" {
				contextPath = fromConfig(cfg.Build.Context)
			}
		default:
			contextPath = fromConfig(cfg.Build.Context)
			dockerfilePath = filepath.Join(contextPath, "Dockerfile")
		}
		plan := NewDockerfilePlan(dockerfilePath, contextPath)
		plan.Args = cfg.Build.Args
		plan.Target = cfg.Build.Target
		plan.CacheFrom = cfg.Build.CacheFrom.List()
		plan.Options = cfg.Build.Options
		return plan
	case PlanTypeCompose:
		plan := NewComposePlan(composeFilePaths(cfg, configPath), cfg.Service, resourceName)
		plan.RunServices = cfg.RunServices
		return plan
	}
	return NewImagePlan(cfg.Image)
}

// resolveUsers sets the remote, container and effective users and the host
// user's IDs.
func resolveUsers(resolved *ResolvedDevContainer, cfg *DevContainerConfig) {
	resolved.RemoteUser = cfg.RemoteUser
	resolved.ContainerUser = cfg.ContainerUser
	resolved.EffectiveUser = resolved.RemoteUser
	if resolved.EffectiveUser == "" {
		resolved.EffectiveUser = resolved.ContainerUser
	}
	resolved.HostUID = os.Getuid()
	resolved.HostGID = os.Getgid()
}

// resolveRuntime sets the environment, runtime options, runArgs, ports and
// mounts.
func (b *Builder) resolveRuntime(resolved *ResolvedDevContainer, cfg *DevContainerConfig, workspaceRoot string) {
	for k, v := range cfg.ContainerEnv {
		resolved.ContainerEnv[k] = v
	}
	for k, v := range cfg.RemoteEnv {
		if v == nil {
			resolved.RemoteEnvUnset = append(resolved.RemoteEnvUnset, k)
		} else {
			resolved.RemoteEnv[k] = *v
		}
	}
	sort.Strings(resolved.RemoteEnvUnset)
	resolved.CapAdd = cfg.CapAdd
	resolved.SecurityOpt = cfg.SecurityOpt
	if cfg.Privileged != nil {
		resolved.Privileged = *cfg.Privileged
	}
	if cfg.Init != nil {
		resolved.Init = *cfg.Init
	}
	if len(cfg.RunArgs) > 0 {
		resolved.RunArgs = ParseRunArgs(cfg.RunArgs)
		for _, arg := range resolved.RunArgs.Ignored {
			b.report.Warnf("runArgs: ignoring %s (dcx manages the container's name and lifetime)", arg)
		}
		if cfg.PlanType() == PlanTypeCompose {
			b.report.Warnf("runArgs are not applied to Docker Compose configurations; set these options in the compose file")
		}
	}
	resolved.ForwardPorts = b.parsePorts("forwardPorts", cfg.ForwardPorts, false)
	resolved.AppPorts = b.parsePorts("appPort", cfg.AppPort, true)
	resolved.Mounts = b.parseMounts(cfg.Mounts, workspaceRoot)
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
			if !slices.Contains(resolved.CapAdd, c) {
				resolved.CapAdd = append(resolved.CapAdd, c)
			}
		}
		for _, opt := range feat.Metadata.SecurityOpt {
			if !slices.Contains(resolved.SecurityOpt, opt) {
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
	resolved.ConfigHash, resolved.InputsHash = configHash, workspaceInputsHash(inputsHash, resolved)
	return nil
}

// workspaceInputsHash adds the default workspace mount to the inputs hash
// when it is not the plain bind of the workspace at the workspace folder
// (which the config and the workspace path already determine): when the
// workspace is inside a git repository, or workspaceFolder is set. A
// container created with another layout then reports drift.
func workspaceInputsHash(inputsHash string, resolved *ResolvedDevContainer) string {
	m := resolved.WorkspaceMount
	if m == nil || resolved.Local == nil || resolved.Local.WorkspaceMount != nil ||
		(m.Source == resolved.LocalRoot && m.Target == resolved.WorkspaceFolder) {
		return inputsHash
	}
	sum := sha256.Sum256([]byte(inputsHash + "\x00workspace\x00" + m.Source + "\x00" + m.Target))
	return hex.EncodeToString(sum[:])
}

// populateBuildDecisions populates build-time decisions.
func (b *Builder) populateBuildDecisions(resolved *ResolvedDevContainer, cfg *DevContainerConfig) {
	resolved.ShouldUpdateUID = ShouldUpdateUID(cfg.UpdateRemoteUserUID, resolved.EffectiveUser, resolved.HostUID, runtime.GOOS)
}

// ShouldUpdateUID decides whether dcx adds the UID update layer, which maps
// the remote user's UID/GID to the host user's so bind-mounted files have
// the right owner. It is the only place this is decided:
//   - only on Linux hosts (as the spec and reference CLI do: Docker
//     Desktop's file sharing maps ownership itself, and remapping to
//     macOS's GID 20 collides with dialout on Debian images);
//   - updateRemoteUserUID: false turns it off (it defaults to true);
//   - never for root (as the remote user, or as the host user).
func ShouldUpdateUID(configured *bool, effectiveUser string, hostUID int, goos string) bool {
	if goos != "linux" || (configured != nil && !*configured) {
		return false
	}
	if effectiveUser == "" || effectiveUser == "root" || effectiveUser == "0" {
		return false
	}
	return hostUID != 0
}

// parseMounts validates config mounts (parsed when the config was read and
// again after substitution) and defaults the Type field.
//
// Relative bind sources ("./data:/data") are resolved against the workspace
// root and "~" against the home directory: Docker rejects both as given.
func (b *Builder) parseMounts(mounts []Mount, workspaceRoot string) []Mount {
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
		if m.Type == "bind" {
			m.Source = absoluteBindSource(m.Source, workspaceRoot)
		}
		result = mergeMountByTarget(result, m)
	}
	return result
}

// absoluteBindSource resolves a relative or "~" bind source.
func absoluteBindSource(src, workspaceRoot string) string {
	switch {
	case src == "" || filepath.IsAbs(src):
		return src
	case src == "~" || strings.HasPrefix(src, "~/"):
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(src, "~"))
		}
		return src
	default:
		return filepath.Join(workspaceRoot, src)
	}
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
