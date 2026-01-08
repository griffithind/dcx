package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/mount"
	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	dcxerrors "github.com/griffithind/dcx/internal/errors"
)

// Builder constructs a Workspace from configuration and resolves all references.
type Builder struct {
	logger *slog.Logger
}

// NewBuilder creates a new workspace builder.
func NewBuilder(logger *slog.Logger) *Builder {
	return &Builder{logger: logger}
}

// BuildOptions contains options for building a workspace.
type BuildOptions struct {
	// ConfigPath is the path to devcontainer.json
	ConfigPath string

	// WorkspaceRoot is the workspace root directory
	WorkspaceRoot string

	// Config is the parsed devcontainer configuration
	Config *config.DevcontainerConfig

	// FeatureResolver resolves features (optional, can be nil for basic workspace)
	FeatureResolver FeatureResolver

	// SubstitutionContext provides variable substitution values
	SubstitutionContext *SubstitutionContext

	// ProjectName overrides the workspace name (from dcx.json)
	ProjectName string
}

// FeatureResolver resolves features to their local paths and metadata.
type FeatureResolver interface {
	Resolve(ctx context.Context, featureRef string, options map[string]interface{}) (*ResolvedFeature, error)
}

// SubstitutionContext provides values for variable substitution.
type SubstitutionContext struct {
	LocalWorkspaceFolder          string
	ContainerWorkspaceFolder      string
	LocalWorkspaceFolderBasename  string
	ContainerWorkspaceFolderBasename string
	DevcontainerID                string
	UserHome                      string
	LocalEnv                      func(string) string
	ContainerEnv                  map[string]string
}

// Build creates a Workspace from the given options.
func (b *Builder) Build(ctx context.Context, opts BuildOptions) (*Workspace, error) {
	if opts.Config == nil {
		return nil, dcxerrors.New(dcxerrors.CategoryConfig, dcxerrors.CodeConfigMissing, "configuration is required")
	}

	ws := New()

	// Set identity
	ws.ConfigPath = opts.ConfigPath
	ws.ConfigDir = filepath.Dir(opts.ConfigPath)
	ws.LocalRoot = opts.WorkspaceRoot
	ws.ID = ComputeID(opts.WorkspaceRoot)
	// Use project name from dcx.json if provided, otherwise compute from config
	if opts.ProjectName != "" {
		ws.Name = opts.ProjectName
	} else {
		ws.Name = ComputeName(opts.WorkspaceRoot, opts.Config)
	}
	ws.RawConfig = opts.Config

	// Build substitution context if not provided
	subCtx := opts.SubstitutionContext
	if subCtx == nil {
		subCtx = &SubstitutionContext{
			LocalWorkspaceFolder:         opts.WorkspaceRoot,
			LocalWorkspaceFolderBasename: filepath.Base(opts.WorkspaceRoot),
			DevcontainerID:               ws.ID,
			LocalEnv:                     os.Getenv,
		}
		// Set container workspace folder (default or from config)
		if opts.Config.WorkspaceFolder != "" {
			subCtx.ContainerWorkspaceFolder = opts.Config.WorkspaceFolder
		} else {
			subCtx.ContainerWorkspaceFolder = "/workspaces/" + filepath.Base(opts.WorkspaceRoot)
		}
		subCtx.ContainerWorkspaceFolderBasename = filepath.Base(subCtx.ContainerWorkspaceFolder)

		// Set user home
		if home, err := os.UserHomeDir(); err == nil {
			subCtx.UserHome = home
		}
	}

	// Determine plan type
	ws.Resolved.PlanType = GetPlanType(opts.Config)

	// Resolve configuration based on plan type
	if err := b.resolveConfig(ctx, ws, opts.Config, subCtx); err != nil {
		return nil, err
	}

	// Resolve features if resolver provided
	if opts.FeatureResolver != nil && len(opts.Config.Features) > 0 {
		if err := b.resolveFeatures(ctx, ws, opts.Config, opts.FeatureResolver); err != nil {
			return nil, err
		}
	}

	// Compute hashes
	if err := b.computeHashes(ws, opts.Config); err != nil {
		return nil, err
	}

	return ws, nil
}

// resolveConfig resolves the base configuration.
func (b *Builder) resolveConfig(ctx context.Context, ws *Workspace, cfg *config.DevcontainerConfig, subCtx *SubstitutionContext) error {
	resolved := ws.Resolved

	// Service name (sanitized for Docker container naming requirements)
	resolved.ServiceName = docker.SanitizeProjectName(ws.Name)

	// Workspace paths
	resolved.WorkspaceFolder = subCtx.ContainerWorkspaceFolder
	if cfg.WorkspaceMount != "" {
		resolved.WorkspaceMount = substituteVars(cfg.WorkspaceMount, subCtx)
	}

	// User configuration
	resolved.RemoteUser = cfg.RemoteUser
	resolved.ContainerUser = cfg.ContainerUser
	if cfg.UpdateRemoteUserUID != nil {
		resolved.UpdateRemoteUserUID = *cfg.UpdateRemoteUserUID
	}

	// Environment variables (with substitution)
	for k, v := range cfg.ContainerEnv {
		resolved.ContainerEnv[k] = substituteVars(v, subCtx)
	}
	for k, v := range cfg.RemoteEnv {
		resolved.RemoteEnv[k] = substituteVars(v, subCtx)
	}

	// Runtime options
	resolved.CapAdd = cfg.CapAdd
	resolved.SecurityOpt = cfg.SecurityOpt
	if cfg.Privileged != nil {
		resolved.Privileged = *cfg.Privileged
	}
	if cfg.Init != nil {
		resolved.Init = *cfg.Init
	}
	resolved.RunArgs = cfg.RunArgs

	// Mounts (with substitution)
	for _, m := range cfg.Mounts {
		resolved.Mounts = append(resolved.Mounts, mount.Mount{
			Type:     mount.Type(m.Type),
			Source:   substituteVars(m.Source, subCtx),
			Target:   substituteVars(m.Target, subCtx),
			ReadOnly: m.ReadOnly,
		})
	}

	// Ports
	resolved.ForwardPorts = parsePortForwards(cfg.GetForwardPorts())
	resolved.AppPorts = parsePortForwards(cfg.GetAppPorts())

	// Lifecycle hooks
	resolved.Hooks = parseLifecycleHooks(cfg)

	// Customizations
	resolved.Customizations = cfg.Customizations

	// GPU requirements
	if cfg.HostRequirements != nil {
		resolved.GPURequirements = parseGPURequirements(cfg.HostRequirements)
	}

	// Plan-specific configuration
	switch resolved.PlanType {
	case PlanTypeImage:
		resolved.Image = cfg.Image
		resolved.FinalImage = cfg.Image

	case PlanTypeDockerfile:
		resolved.Dockerfile = &DockerfilePlan{
			Path:      filepath.Join(ws.ConfigDir, cfg.Build.Dockerfile),
			Context:   filepath.Join(ws.ConfigDir, cfg.Build.Context),
			Args:      cfg.Build.Args,
			Target:    cfg.Build.Target,
			CacheFrom: cfg.Build.CacheFrom,
			Options:   cfg.Build.Options,
		}
		// Image will be set during build

	case PlanTypeCompose:
		composeFiles := cfg.GetDockerComposeFiles()
		absolutePaths := make([]string, len(composeFiles))
		for i, f := range composeFiles {
			absolutePaths[i] = filepath.Join(ws.ConfigDir, f)
		}
		resolved.Compose = &ComposePlan{
			Files:       absolutePaths,
			Service:     cfg.Service,
			RunServices: cfg.RunServices,
			ProjectName: docker.SanitizeProjectName(ws.Name),
			WorkDir:     ws.ConfigDir,
		}
		resolved.ServiceName = cfg.Service
	}

	return nil
}

// resolveFeatures resolves all features in the configuration.
func (b *Builder) resolveFeatures(ctx context.Context, ws *Workspace, cfg *config.DevcontainerConfig, resolver FeatureResolver) error {
	features := make([]*ResolvedFeature, 0, len(cfg.Features))

	for featureRef, options := range cfg.Features {
		opts, ok := options.(map[string]interface{})
		if !ok {
			opts = make(map[string]interface{})
		}

		resolved, err := resolver.Resolve(ctx, featureRef, opts)
		if err != nil {
			return dcxerrors.FeatureResolve(featureRef, err)
		}

		features = append(features, resolved)
	}

	// Sort by installation order
	sort.Slice(features, func(i, j int) bool {
		return features[i].InstallOrder < features[j].InstallOrder
	})

	ws.Resolved.Features = features

	// Merge feature metadata into resolved config
	b.mergeFeatureMetadata(ws)

	return nil
}

// mergeFeatureMetadata merges feature metadata into the resolved config.
func (b *Builder) mergeFeatureMetadata(ws *Workspace) {
	for _, f := range ws.Resolved.Features {
		if f.Metadata == nil {
			continue
		}

		// CapAdd: union
		ws.Resolved.CapAdd = unionStrings(ws.Resolved.CapAdd, f.Metadata.CapAdd)

		// SecurityOpt: union
		ws.Resolved.SecurityOpt = unionStrings(ws.Resolved.SecurityOpt, f.Metadata.SecurityOpt)

		// Privileged: true wins
		if f.Metadata.Privileged {
			ws.Resolved.Privileged = true
		}

		// Init: true wins
		if f.Metadata.Init {
			ws.Resolved.Init = true
		}

		// ContainerEnv: feature doesn't override base
		for k, v := range f.Metadata.ContainerEnv {
			if _, exists := ws.Resolved.ContainerEnv[k]; !exists {
				ws.Resolved.ContainerEnv[k] = v
			}
		}

		// Mounts: append
		for _, m := range f.Metadata.Mounts {
			ws.Resolved.Mounts = append(ws.Resolved.Mounts, mount.Mount{
				Type:     mount.Type(m.Type),
				Source:   m.Source,
				Target:   m.Target,
				ReadOnly: m.ReadOnly,
			})
		}

		// Customizations: deep merge
		deepMergeCustomizations(ws.Resolved.Customizations, f.Metadata.Customizations)
	}
}

// computeHashes computes all configuration hashes.
func (b *Builder) computeHashes(ws *Workspace, cfg *config.DevcontainerConfig) error {
	hashes := ws.Hashes

	// Config hash (from raw JSON if available, otherwise marshal)
	if raw := cfg.GetRawJSON(); len(raw) > 0 {
		hashes.Config = hashBytes(raw)
	} else {
		data, err := json.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("marshal config for hash: %w", err)
		}
		hashes.Config = hashBytes(data)
	}

	// Dockerfile hash
	if ws.Resolved.Dockerfile != nil {
		if content, err := os.ReadFile(ws.Resolved.Dockerfile.Path); err == nil {
			hashes.Dockerfile = hashBytes(content)
		}
	}

	// Compose hash (hash all compose files)
	if ws.Resolved.Compose != nil {
		var combined []byte
		for _, f := range ws.Resolved.Compose.Files {
			if content, err := os.ReadFile(f); err == nil {
				combined = append(combined, content...)
			}
		}
		if len(combined) > 0 {
			hashes.Compose = hashBytes(combined)
		}
	}

	// Features hash
	if len(ws.Resolved.Features) > 0 {
		var featureData []string
		for _, f := range ws.Resolved.Features {
			// Include ID, digest, and options in hash
			optData, _ := json.Marshal(f.Options)
			featureData = append(featureData, fmt.Sprintf("%s:%s:%s", f.ID, f.Digest, string(optData)))
		}
		sort.Strings(featureData)
		hashes.Features = hashBytes([]byte(strings.Join(featureData, "|")))
	}

	// Overall hash (combine all hashes)
	overall := fmt.Sprintf("%s|%s|%s|%s",
		hashes.Config,
		hashes.Dockerfile,
		hashes.Compose,
		hashes.Features)
	hashes.Overall = hashBytes([]byte(overall))

	return nil
}

// Helper functions

func hashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func substituteVars(s string, ctx *SubstitutionContext) string {
	if ctx == nil {
		return s
	}

	// Simple variable substitution
	replacements := map[string]string{
		"${localWorkspaceFolder}":          ctx.LocalWorkspaceFolder,
		"${containerWorkspaceFolder}":      ctx.ContainerWorkspaceFolder,
		"${localWorkspaceFolderBasename}":  ctx.LocalWorkspaceFolderBasename,
		"${containerWorkspaceFolderBasename}": ctx.ContainerWorkspaceFolderBasename,
		"${devcontainerId}":                ctx.DevcontainerID,
		"${userHome}":                      ctx.UserHome,
	}

	for old, new := range replacements {
		s = strings.ReplaceAll(s, old, new)
	}

	// Handle ${localEnv:VAR} and ${localEnv:VAR:default}
	s = substituteEnvVars(s, "localEnv", ctx.LocalEnv)
	s = substituteEnvVars(s, "env", ctx.LocalEnv) // alias

	// Handle ${containerEnv:VAR}
	if ctx.ContainerEnv != nil {
		s = substituteEnvVars(s, "containerEnv", func(key string) string {
			return ctx.ContainerEnv[key]
		})
	}

	return s
}

func substituteEnvVars(s string, prefix string, getEnv func(string) string) string {
	// Pattern: ${prefix:VAR} or ${prefix:VAR:default}
	pattern := "${" + prefix + ":"
	result := s

	for {
		start := strings.Index(result, pattern)
		if start == -1 {
			break
		}

		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start

		// Extract variable part: VAR or VAR:default
		varPart := result[start+len(pattern) : end]
		parts := strings.SplitN(varPart, ":", 2)
		varName := parts[0]
		defaultVal := ""
		if len(parts) > 1 {
			defaultVal = parts[1]
		}

		// Get value
		value := ""
		if getEnv != nil {
			value = getEnv(varName)
		}
		if value == "" {
			value = defaultVal
		}

		result = result[:start] + value + result[end+1:]
	}

	return result
}

func parsePortForwards(ports []string) []PortForward {
	if len(ports) == 0 {
		return nil
	}

	result := make([]PortForward, 0, len(ports))
	for _, p := range ports {
		pf := PortForward{}
		// Parse "hostPort:containerPort" or just "port"
		parts := strings.Split(p, ":")
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%d", &pf.HostPort)
			fmt.Sscanf(parts[1], "%d", &pf.ContainerPort)
		} else {
			fmt.Sscanf(p, "%d", &pf.ContainerPort)
			pf.HostPort = pf.ContainerPort
		}
		if pf.ContainerPort > 0 {
			result = append(result, pf)
		}
	}
	return result
}

func parseLifecycleHooks(cfg *config.DevcontainerConfig) *LifecycleHooks {
	hooks := &LifecycleHooks{
		WaitFor: cfg.WaitFor,
	}

	hooks.Initialize = parseHookCommand(cfg.InitializeCommand)
	hooks.OnCreate = parseHookCommand(cfg.OnCreateCommand)
	hooks.UpdateContent = parseHookCommand(cfg.UpdateContentCommand)
	hooks.PostCreate = parseHookCommand(cfg.PostCreateCommand)
	hooks.PostStart = parseHookCommand(cfg.PostStartCommand)
	hooks.PostAttach = parseHookCommand(cfg.PostAttachCommand)

	return hooks
}

func parseHookCommand(cmd interface{}) []HookCommand {
	if cmd == nil {
		return nil
	}

	switch v := cmd.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []HookCommand{{Command: v}}

	case []interface{}:
		if len(v) == 0 {
			return nil
		}
		args := make([]string, len(v))
		for i, a := range v {
			args[i] = fmt.Sprint(a)
		}
		return []HookCommand{{Args: args}}

	case []string:
		if len(v) == 0 {
			return nil
		}
		return []HookCommand{{Args: v}}

	case map[string]interface{}:
		if len(v) == 0 {
			return nil
		}
		result := make([]HookCommand, 0, len(v))
		for name, c := range v {
			cmd := HookCommand{Name: name, Parallel: true}
			switch cv := c.(type) {
			case string:
				cmd.Command = cv
			case []interface{}:
				args := make([]string, len(cv))
				for i, a := range cv {
					args[i] = fmt.Sprint(a)
				}
				cmd.Args = args
			}
			result = append(result, cmd)
		}
		return result

	default:
		return nil
	}
}

func parseGPURequirements(hr *config.HostRequirements) *GPURequirements {
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

func unionStrings(a, b []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(a)+len(b))

	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

func deepMergeCustomizations(target, source map[string]interface{}) {
	if source == nil {
		return
	}

	for tool, sourceConfig := range source {
		if targetConfig, exists := target[tool]; exists {
			// Both have this tool - deep merge
			targetMap, targetOK := targetConfig.(map[string]interface{})
			sourceMap, sourceOK := sourceConfig.(map[string]interface{})
			if targetOK && sourceOK {
				deepMergeToolConfig(targetMap, sourceMap)
				continue
			}
		}
		target[tool] = sourceConfig
	}
}

func deepMergeToolConfig(target, source map[string]interface{}) {
	for key, sourceVal := range source {
		if targetVal, exists := target[key]; exists {
			// Array union for extensions
			if key == "extensions" {
				target[key] = unionInterfaces(targetVal, sourceVal)
				continue
			}
			// Map merge for settings (target wins for same key)
			if targetMap, ok := targetVal.(map[string]interface{}); ok {
				if sourceMap, ok := sourceVal.(map[string]interface{}); ok {
					for k, v := range sourceMap {
						if _, exists := targetMap[k]; !exists {
							targetMap[k] = v
						}
					}
					continue
				}
			}
		}
		target[key] = sourceVal
	}
}

func unionInterfaces(a, b interface{}) []interface{} {
	seen := make(map[string]bool)
	result := []interface{}{}

	addItems := func(items interface{}) {
		if arr, ok := items.([]interface{}); ok {
			for _, item := range arr {
				key := fmt.Sprint(item)
				if !seen[key] {
					seen[key] = true
					result = append(result, item)
				}
			}
		}
	}

	addItems(a)
	addItems(b)
	return result
}
