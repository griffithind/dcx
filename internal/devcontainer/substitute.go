package devcontainer

import (
	"os"
	"path/filepath"
	"regexp"
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

// SubstituteConfig performs variable substitution on the entire config.
func SubstituteConfig(cfg *DevContainerConfig, ctx *SubstitutionContext) {
	if cfg == nil || ctx == nil {
		return
	}

	// Substitute in string fields
	cfg.Image = Substitute(cfg.Image, ctx)
	cfg.WorkspaceFolder = Substitute(cfg.WorkspaceFolder, ctx)
	cfg.WorkspaceMount = Substitute(cfg.WorkspaceMount, ctx)
	cfg.RemoteUser = Substitute(cfg.RemoteUser, ctx)

	// Substitute in build config
	if cfg.Build != nil {
		cfg.Build.Dockerfile = Substitute(cfg.Build.Dockerfile, ctx)
		cfg.Build.Context = Substitute(cfg.Build.Context, ctx)
		for k, v := range cfg.Build.Args {
			cfg.Build.Args[k] = Substitute(v, ctx)
		}
	}

	// Substitute in environment maps
	for k, v := range cfg.ContainerEnv {
		cfg.ContainerEnv[k] = Substitute(v, ctx)
	}
	for k, v := range cfg.RemoteEnv {
		cfg.RemoteEnv[k] = Substitute(v, ctx)
	}

	// Substitute in mounts
	for i := range cfg.Mounts {
		cfg.Mounts[i].Source = Substitute(cfg.Mounts[i].Source, ctx)
		cfg.Mounts[i].Target = Substitute(cfg.Mounts[i].Target, ctx)
		if cfg.Mounts[i].Raw != "" {
			cfg.Mounts[i].Raw = Substitute(cfg.Mounts[i].Raw, ctx)
		}
	}

	// Substitute in runArgs
	for i, arg := range cfg.RunArgs {
		cfg.RunArgs[i] = Substitute(arg, ctx)
	}
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
