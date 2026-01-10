package devcontainer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/griffithind/dcx/internal/common"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/state"
)

// Builder constructs a ResolvedDevContainer from configuration and resolves all references.
// This replaces the previous workspace.Builder.
type Builder struct {
	logger *slog.Logger
}

// NewBuilder creates a new devcontainer builder.
func NewBuilder(logger *slog.Logger) *Builder {
	return &Builder{logger: logger}
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

	// ProjectName overrides the workspace name (from dcx.json)
	ProjectName string
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

	// Use project name from dcx.json if provided, otherwise compute from config
	if opts.ProjectName != "" {
		resolved.Name = opts.ProjectName
	} else {
		resolved.Name = ComputeName(opts.WorkspaceRoot, opts.Config)
	}
	resolved.RawConfig = opts.Config

	// Build substitution context if not provided
	subCtx := opts.SubstitutionContext
	if subCtx == nil {
		subCtx = &SubstitutionContext{
			LocalWorkspaceFolder: opts.WorkspaceRoot,
			DevcontainerID:       resolved.ID,
			LocalEnv:             os.Getenv,
		}
		// Set container workspace folder (default or from config)
		if opts.Config.WorkspaceFolder != "" {
			subCtx.ContainerWorkspaceFolder = opts.Config.WorkspaceFolder
		} else {
			subCtx.ContainerWorkspaceFolder = "/workspaces/" + filepath.Base(opts.WorkspaceRoot)
		}

		// Set user home
		if home, err := os.UserHomeDir(); err == nil {
			subCtx.UserHome = home
		}
	}

	// Create execution plan based on config type
	planType := opts.Config.PlanType()
	switch planType {
	case PlanTypeImage:
		resolved.Plan = NewImagePlan(opts.Config.Image)
		resolved.Image = opts.Config.Image
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
		composeFiles := opts.Config.GetDockerComposeFiles()
		absolutePaths := make([]string, len(composeFiles))
		for i, f := range composeFiles {
			absolutePaths[i] = filepath.Join(resolved.ConfigDir, f)
		}
		resolved.Plan = NewComposePlan(
			absolutePaths,
			opts.Config.Service,
			common.SanitizeProjectName(resolved.Name),
		)
	}

	// Resolve workspace paths
	resolved.WorkspaceFolder = subCtx.ContainerWorkspaceFolder
	if opts.Config.WorkspaceMount != "" {
		resolved.WorkspaceMount = Substitute(opts.Config.WorkspaceMount, subCtx)
	}

	// User configuration
	resolved.RemoteUser = Substitute(opts.Config.RemoteUser, subCtx)
	resolved.ContainerUser = Substitute(opts.Config.ContainerUser, subCtx)
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
		resolved.ContainerEnv[k] = Substitute(v, subCtx)
	}
	for k, v := range opts.Config.RemoteEnv {
		resolved.RemoteEnv[k] = Substitute(v, subCtx)
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
	resolved.RunArgs = opts.Config.RunArgs

	// Forward ports
	resolved.ForwardPorts = parseForwardPorts(opts.Config.ForwardPorts)

	// Mounts
	resolved.Mounts = parseMounts(opts.Config.Mounts)

	// Service name (sanitized for Docker)
	resolved.ServiceName = common.SanitizeProjectName(resolved.Name)

	// Customizations
	resolved.Customizations = opts.Config.Customizations

	// GPU requirements
	if opts.Config.HostRequirements != nil {
		resolved.GPURequirements = parseGPURequirements(opts.Config.HostRequirements)
	}

	// Resolve features if any exist
	if len(opts.Config.Features) > 0 {
		if err := b.resolveFeatures(ctx, resolved, opts.Config); err != nil {
			return nil, err
		}
	}

	// Compute hashes
	if err := b.computeHashes(resolved, opts.Config); err != nil {
		return nil, err
	}

	// Populate build decisions
	b.populateBuildDecisions(resolved, opts.Config)

	// Initialize labels
	resolved.Labels = state.NewContainerLabels()

	return resolved, nil
}

// resolveFeatures resolves all features from the configuration.
func (b *Builder) resolveFeatures(ctx context.Context, resolved *ResolvedDevContainer, cfg *DevContainerConfig) error {
	mgr, err := features.NewManager(resolved.ConfigDir)
	if err != nil {
		return fmt.Errorf("failed to create feature manager: %w", err)
	}

	feats, err := mgr.ResolveAll(ctx, cfg.Features, cfg.OverrideFeatureInstallOrder)
	if err != nil {
		return fmt.Errorf("failed to resolve features: %w", err)
	}

	resolved.Features = feats

	// Merge feature mounts, capAdd, securityOpt, etc.
	b.mergeFeatureRuntimeConfig(resolved, feats)

	return nil
}

// mergeFeatureRuntimeConfig merges runtime configuration from features into resolved.
func (b *Builder) mergeFeatureRuntimeConfig(resolved *ResolvedDevContainer, feats []*features.Feature) {
	for _, feat := range feats {
		if feat.Metadata == nil {
			continue
		}

		// Merge mounts
		for _, fm := range feat.Metadata.Mounts {
			if fm.Target == "" {
				continue
			}

			dm := mount.Mount{
				Source: fm.Source,
				Target: fm.Target,
			}

			// Set mount type (default to bind)
			switch fm.Type {
			case "volume":
				dm.Type = mount.TypeVolume
			case "tmpfs":
				dm.Type = mount.TypeTmpfs
			default:
				dm.Type = mount.TypeBind
			}

			resolved.Mounts = append(resolved.Mounts, dm)
		}

		// Merge capAdd
		for _, cap := range feat.Metadata.CapAdd {
			if !contains(resolved.CapAdd, cap) {
				resolved.CapAdd = append(resolved.CapAdd, cap)
			}
		}

		// Merge securityOpt
		for _, opt := range feat.Metadata.SecurityOpt {
			if !contains(resolved.SecurityOpt, opt) {
				resolved.SecurityOpt = append(resolved.SecurityOpt, opt)
			}
		}

		// Merge privileged (OR: if any feature needs privileged, enable it)
		if feat.Metadata.Privileged {
			resolved.Privileged = true
		}

		// Merge init (OR: if any feature needs init, enable it)
		if feat.Metadata.Init {
			resolved.Init = true
		}

		// Merge containerEnv
		for k, v := range feat.Metadata.ContainerEnv {
			if _, exists := resolved.ContainerEnv[k]; !exists {
				resolved.ContainerEnv[k] = v
			}
		}
	}
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

// computeHashes computes all configuration hashes.
func (b *Builder) computeHashes(resolved *ResolvedDevContainer, cfg *DevContainerConfig) error {
	var dockerfilePath string
	var composeFiles []string

	if df, ok := resolved.Plan.(*DockerfilePlan); ok {
		dockerfilePath = df.Dockerfile
	}
	if cp, ok := resolved.Plan.(*ComposePlan); ok {
		composeFiles = cp.Files
	}

	hashes, err := ComputeAllHashes(cfg, dockerfilePath, composeFiles, resolved.Features)
	if err != nil {
		return err
	}

	resolved.Hashes = hashes

	// Set derived image tag based on hash
	if hashes.Config != "" && len(hashes.Config) >= 12 {
		resolved.DerivedImage = fmt.Sprintf("dcx/%s:%s-features", resolved.ID, hashes.Config[:12])
	}

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
	case map[string]interface{}:
		gpu.Enabled = true
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

// parseForwardPorts converts config forwardPorts to resolved PortForward slice.
func parseForwardPorts(ports []interface{}) []PortForward {
	if len(ports) == 0 {
		return nil
	}

	result := make([]PortForward, 0, len(ports))
	for _, port := range ports {
		var pf PortForward
		switch v := port.(type) {
		case float64:
			pf.ContainerPort = int(v)
			pf.HostPort = int(v)
		case int:
			pf.ContainerPort = v
			pf.HostPort = v
		case string:
			// Parse "hostPort:containerPort" or just "port"
			parts := strings.Split(v, ":")
			if len(parts) == 2 {
				if hp, err := strconv.Atoi(parts[0]); err == nil {
					pf.HostPort = hp
				}
				if cp, err := strconv.Atoi(parts[1]); err == nil {
					pf.ContainerPort = cp
				}
			} else if len(parts) == 1 {
				if p, err := strconv.Atoi(parts[0]); err == nil {
					pf.ContainerPort = p
					pf.HostPort = p
				}
			}
		}
		if pf.ContainerPort > 0 {
			result = append(result, pf)
		}
	}
	return result
}

// parseMounts converts config mounts to Docker API mount.Mount slice.
func parseMounts(mounts []Mount) []mount.Mount {
	if len(mounts) == 0 {
		return nil
	}

	result := make([]mount.Mount, 0, len(mounts))
	for _, m := range mounts {
		if m.Target == "" {
			continue
		}

		dm := mount.Mount{
			Source: m.Source,
			Target: m.Target,
		}

		// Set mount type (default to bind)
		switch m.Type {
		case "volume":
			dm.Type = mount.TypeVolume
		case "tmpfs":
			dm.Type = mount.TypeTmpfs
		default:
			dm.Type = mount.TypeBind
		}

		// Set read-only
		dm.ReadOnly = m.ReadOnly

		result = append(result, dm)
	}
	return result
}
