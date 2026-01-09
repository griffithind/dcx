package config

import (
	"os"
	"path/filepath"
	"regexp"
)

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

// RegisterSubstitution adds a custom substitution pattern.
// This allows extensions to add their own patterns.
func RegisterSubstitution(pattern string, handler func(match []string, ctx *SubstitutionContext) string) error {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	substitutions = append(substitutions, substitution{
		pattern: re,
		handler: handler,
	})
	return nil
}
