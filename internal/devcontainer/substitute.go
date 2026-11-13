package devcontainer

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SubstitutionContext provides values for variable substitution.
type SubstitutionContext struct {
	LocalWorkspaceFolder     string
	ContainerWorkspaceFolder string
	DevcontainerID           string
	UserHome                 string              // User's home directory for ${userHome}
	ContainerEnv             map[string]string   // Container environment variables for ${containerEnv:VAR}
	LocalEnv                 func(string) string // Optional function to get local env vars; falls back to os.Getenv
}

// substitution represents a single variable substitution pattern.
type substitution struct {
	pattern *regexp.Regexp
	handler func(match []string, ctx *SubstitutionContext) string
}

// substitutions is the registry of all variable substitution patterns.
// Order matters - more specific patterns should come before general ones.
var substitutions = []substitution{
	// Environment variable patterns with optional defaults
	{
		pattern: regexp.MustCompile(`\$\{localEnv:([^}:]+)(?::([^}]*))?\}`),
		handler: handleLocalEnv,
	},
	{
		pattern: regexp.MustCompile(`\$\{env:([^}:]+)(?::([^}]*))?\}`),
		handler: handleLocalEnv, // env is alias for localEnv
	},
	{
		pattern: regexp.MustCompile(`\$\{containerEnv:([^}:]+)(?::([^}]*))?\}`),
		handler: handleContainerEnv,
	},

	// Workspace folder patterns
	{
		pattern: regexp.MustCompile(`\$\{localWorkspaceFolder\}`),
		handler: handleLocalWorkspaceFolder,
	},
	{
		pattern: regexp.MustCompile(`\$\{containerWorkspaceFolder\}`),
		handler: handleContainerWorkspaceFolder,
	},
	{
		pattern: regexp.MustCompile(`\$\{localWorkspaceFolderBasename\}`),
		handler: handleLocalWorkspaceFolderBasename,
	},
	{
		pattern: regexp.MustCompile(`\$\{containerWorkspaceFolderBasename\}`),
		handler: handleContainerWorkspaceFolderBasename,
	},

	// Other patterns
	{
		pattern: regexp.MustCompile(`\$\{devcontainerId\}`),
		handler: handleDevcontainerId,
	},
	{
		pattern: regexp.MustCompile(`\$\{pathSeparator\}`),
		handler: handlePathSeparator,
	},
	{
		pattern: regexp.MustCompile(`\$\{userHome\}`),
		handler: handleUserHome,
	},
}

// Handler functions for each substitution type

func handleLocalEnv(match []string, ctx *SubstitutionContext) string {
	if len(match) < 2 {
		return match[0]
	}
	var value string
	if ctx != nil && ctx.LocalEnv != nil {
		value = ctx.LocalEnv(match[1])
	} else {
		value = os.Getenv(match[1])
	}
	if value == "" && len(match) >= 3 {
		value = match[2] // default value
	}
	return value
}

func handleContainerEnv(match []string, ctx *SubstitutionContext) string {
	if ctx == nil || ctx.ContainerEnv == nil || len(match) < 2 {
		return match[0]
	}
	value := ctx.ContainerEnv[match[1]]
	if value == "" && len(match) >= 3 {
		value = match[2] // default value
	}
	return value
}

func handleLocalWorkspaceFolder(match []string, ctx *SubstitutionContext) string {
	if ctx == nil || ctx.LocalWorkspaceFolder == "" {
		return match[0]
	}
	return ctx.LocalWorkspaceFolder
}

func handleContainerWorkspaceFolder(match []string, ctx *SubstitutionContext) string {
	if ctx == nil || ctx.ContainerWorkspaceFolder == "" {
		return match[0]
	}
	return ctx.ContainerWorkspaceFolder
}

func handleLocalWorkspaceFolderBasename(match []string, ctx *SubstitutionContext) string {
	if ctx == nil || ctx.LocalWorkspaceFolder == "" {
		return match[0]
	}
	return filepath.Base(ctx.LocalWorkspaceFolder)
}

func handleContainerWorkspaceFolderBasename(match []string, ctx *SubstitutionContext) string {
	if ctx == nil || ctx.ContainerWorkspaceFolder == "" {
		return match[0]
	}
	return filepath.Base(ctx.ContainerWorkspaceFolder)
}

func handleDevcontainerId(match []string, ctx *SubstitutionContext) string {
	if ctx == nil || ctx.DevcontainerID == "" {
		return match[0]
	}
	return ctx.DevcontainerID
}

func handlePathSeparator(match []string, ctx *SubstitutionContext) string {
	return string(filepath.Separator)
}

func handleUserHome(match []string, ctx *SubstitutionContext) string {
	if ctx != nil && ctx.UserHome != "" {
		return ctx.UserHome
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return match[0]
}

// substituteWithRegistry performs variable substitution using the registry.
func substituteWithRegistry(s string, ctx *SubstitutionContext) string {
	for _, sub := range substitutions {
		s = sub.pattern.ReplaceAllStringFunc(s, func(match string) string {
			parts := sub.pattern.FindStringSubmatch(match)
			// Handlers return match[0] when they can't perform substitution
			// (e.g., when ctx is nil), otherwise they return the substituted value
			// which may be empty (e.g., env var not found)
			return sub.handler(parts, ctx)
		})
	}
	return s
}

// Substitute performs variable substitution on a string.
func Substitute(s string, ctx *SubstitutionContext) string {
	return substituteWithRegistry(s, ctx)
}

// NewSubstitutionContext builds the substitution context for a workspace.
// It is the single place the context is assembled, so every caller sees the
// same values for ${devcontainerId}, ${userHome}, the workspace folders, etc.
//
// ${containerEnv:VAR} is intentionally left unresolved here: container
// environment only exists once the container runs, so those references are
// resolved at exec time (see ResolveContainerEnvRefs).
func NewSubstitutionContext(workspaceRoot string, cfg *DevContainerConfig) *SubstitutionContext {
	ctx := &SubstitutionContext{
		LocalWorkspaceFolder: workspaceRoot,
		DevcontainerID:       ComputeID(workspaceRoot),
		LocalEnv:             os.Getenv,
	}
	if home, err := os.UserHomeDir(); err == nil {
		ctx.UserHome = home
	}
	if cfg != nil {
		// workspaceFolder may itself reference local variables
		// (e.g. /src/${localWorkspaceFolderBasename}); resolve it before it
		// becomes ${containerWorkspaceFolder}.
		folder := *cfg
		folder.WorkspaceFolder = Substitute(cfg.WorkspaceFolder, ctx)
		ctx.ContainerWorkspaceFolder = DetermineContainerWorkspaceFolder(&folder, workspaceRoot)
	}
	return ctx
}

// SubstituteConfig performs variable substitution on every string-valued
// property of the config. It must run exactly once per loaded config (Load
// does this), regardless of how the config file was located.
//
// String-form mounts are re-parsed from their substituted text, so variables
// containing ':' or ',' cannot corrupt mount parsing.
func SubstituteConfig(cfg *DevContainerConfig, ctx *SubstitutionContext) {
	if cfg == nil || ctx == nil {
		return
	}
	sub := func(s string) string { return Substitute(s, ctx) }
	subSlice := func(in []string) {
		for i := range in {
			in[i] = sub(in[i])
		}
	}
	subMap := func(in map[string]string) {
		for k, v := range in {
			in[k] = sub(v)
		}
	}

	cfg.Name = sub(cfg.Name)
	cfg.Image = sub(cfg.Image)
	cfg.WorkspaceFolder = sub(cfg.WorkspaceFolder)
	cfg.WorkspaceMount = sub(cfg.WorkspaceMount)
	cfg.RemoteUser = sub(cfg.RemoteUser)
	cfg.ContainerUser = sub(cfg.ContainerUser)
	cfg.Service = sub(cfg.Service)
	subSlice(cfg.DockerComposeFile)
	subSlice(cfg.RunServices)

	if cfg.Build != nil {
		cfg.Build.Dockerfile = sub(cfg.Build.Dockerfile)
		cfg.Build.Context = sub(cfg.Build.Context)
		cfg.Build.Target = sub(cfg.Build.Target)
		subMap(cfg.Build.Args)
		subSlice(cfg.Build.CacheFrom)
		subSlice(cfg.Build.Options)
	}

	subMap(cfg.ContainerEnv)
	subMap(cfg.RemoteEnv)
	subSlice(cfg.RunArgs)
	subSlice(cfg.CapAdd)
	subSlice(cfg.SecurityOpt)

	for i := range cfg.Mounts {
		m := &cfg.Mounts[i]
		if m.Raw != "" {
			m.Raw = sub(m.Raw)
			if parsed, err := ParseMountString(m.Raw); err == nil {
				raw := m.Raw
				*m = parsed
				m.Raw = raw
			}
			continue
		}
		m.Source = sub(m.Source)
		m.Target = sub(m.Target)
	}

	// Feature options, once, with the full context (they reach the feature
	// install scripts and the staleness key).
	for ref, opts := range cfg.Features {
		cfg.Features[ref] = substituteAny(opts, sub)
	}

	cfg.ForwardPorts = cfg.ForwardPorts.substitute(sub)
	cfg.AppPort = cfg.AppPort.substitute(sub)

	cfg.InitializeCommand = cfg.InitializeCommand.Substitute(sub)
	cfg.OnCreateCommand = cfg.OnCreateCommand.Substitute(sub)
	cfg.UpdateContentCommand = cfg.UpdateContentCommand.Substitute(sub)
	cfg.PostCreateCommand = cfg.PostCreateCommand.Substitute(sub)
	cfg.PostStartCommand = cfg.PostStartCommand.Substitute(sub)
	cfg.PostAttachCommand = cfg.PostAttachCommand.Substitute(sub)
}

// substituteAny substitutes strings inside a JSON-decoded value (string,
// array or object), leaving numbers and booleans untouched.
func substituteAny(v interface{}, sub func(string) string) interface{} {
	switch t := v.(type) {
	case string:
		return sub(t)
	case []interface{}:
		for i := range t {
			t[i] = substituteAny(t[i], sub)
		}
		return t
	case []string:
		for i := range t {
			t[i] = sub(t[i])
		}
		return t
	case map[string]interface{}:
		for k := range t {
			t[k] = substituteAny(t[k], sub)
		}
		return t
	default:
		return v
	}
}

var containerEnvRef = regexp.MustCompile(`\$\{containerEnv:([^}:]+)(?::([^}]*))?\}`)

// ResolveContainerEnvRefs resolves ${containerEnv:VAR[:default]} references
// in values (typically remoteEnv) against the running container's
// environment. Other text is left unchanged.
func ResolveContainerEnvRefs(values map[string]string, containerEnv map[string]string) map[string]string {
	if len(values) == 0 {
		return values
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		out[k] = containerEnvRef.ReplaceAllStringFunc(v, func(match string) string {
			parts := containerEnvRef.FindStringSubmatch(match)
			if val, ok := containerEnv[parts[1]]; ok && val != "" {
				return val
			}
			return parts[2]
		})
	}
	return out
}

// EnvListToMap converts KEY=VALUE pairs (as reported by docker inspect) to a map.
func EnvListToMap(env []string) map[string]string {
	m := make(map[string]string, len(env))
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok {
			m[k] = v
		}
	}
	return m
}

// DetermineContainerWorkspaceFolder computes the container workspace folder.
// Per spec: default is /workspaces/<basename> for image/dockerfile, "/" for compose.
func DetermineContainerWorkspaceFolder(cfg *DevContainerConfig, localWorkspace string) string {
	if cfg.WorkspaceFolder != "" {
		return cfg.WorkspaceFolder
	}

	// Per spec: compose defaults to "/", image/dockerfile defaults to /workspaces/<basename>
	if cfg.PlanType() == PlanTypeCompose {
		return "/"
	}

	basename := filepath.Base(localWorkspace)
	return "/workspaces/" + basename
}
