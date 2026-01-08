package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tidwall/jsonc"
)

// Parse parses a devcontainer.json file from bytes.
func Parse(data []byte) (*DevcontainerConfig, error) {
	// Strip comments and trailing commas
	stripped := jsonc.ToJSON(data)

	var cfg DevcontainerConfig
	if err := json.Unmarshal(stripped, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse devcontainer.json: %w", err)
	}

	// Store raw JSON for hash computation
	cfg.SetRawJSON(stripped)

	return &cfg, nil
}

// ParseFile parses a devcontainer.json file from a path.
func ParseFile(path string) (*DevcontainerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	return Parse(data)
}

// Variable substitution patterns
var (
	// ${localEnv:VAR} or ${localEnv:VAR:default}
	localEnvPattern = regexp.MustCompile(`\$\{localEnv:([^}:]+)(?::([^}]*))?\}`)

	// ${env:VAR} or ${env:VAR:default} (alias for localEnv)
	envPattern = regexp.MustCompile(`\$\{env:([^}:]+)(?::([^}]*))?\}`)

	// ${containerEnv:VAR} or ${containerEnv:VAR:default}
	containerEnvPattern = regexp.MustCompile(`\$\{containerEnv:([^}:]+)(?::([^}]*))?\}`)

	// ${localWorkspaceFolder}
	localWorkspaceFolderPattern = regexp.MustCompile(`\$\{localWorkspaceFolder\}`)

	// ${containerWorkspaceFolder}
	containerWorkspaceFolderPattern = regexp.MustCompile(`\$\{containerWorkspaceFolder\}`)

	// ${localWorkspaceFolderBasename}
	localWorkspaceFolderBasenamePattern = regexp.MustCompile(`\$\{localWorkspaceFolderBasename\}`)

	// ${devcontainerId}
	devcontainerIdPattern = regexp.MustCompile(`\$\{devcontainerId\}`)

	// ${pathSeparator}
	pathSeparatorPattern = regexp.MustCompile(`\$\{pathSeparator\}`)
)

// SubstitutionContext provides values for variable substitution.
type SubstitutionContext struct {
	LocalWorkspaceFolder     string
	ContainerWorkspaceFolder string
	DevcontainerID           string
	ContainerEnv             map[string]string // Container environment variables for ${containerEnv:VAR}
}

// Substitute performs variable substitution on a string.
func Substitute(s string, ctx *SubstitutionContext) string {
	// ${localEnv:VAR} or ${localEnv:VAR:default}
	s = localEnvPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := localEnvPattern.FindStringSubmatch(match)
		if len(parts) >= 2 {
			value := os.Getenv(parts[1])
			if value == "" && len(parts) >= 3 {
				value = parts[2] // default value
			}
			return value
		}
		return match
	})

	// ${env:VAR} (alias for localEnv)
	s = envPattern.ReplaceAllStringFunc(s, func(match string) string {
		parts := envPattern.FindStringSubmatch(match)
		if len(parts) >= 2 {
			value := os.Getenv(parts[1])
			if value == "" && len(parts) >= 3 {
				value = parts[2]
			}
			return value
		}
		return match
	})

	// ${containerEnv:VAR} or ${containerEnv:VAR:default}
	if ctx != nil && ctx.ContainerEnv != nil {
		s = containerEnvPattern.ReplaceAllStringFunc(s, func(match string) string {
			parts := containerEnvPattern.FindStringSubmatch(match)
			if len(parts) >= 2 {
				value := ctx.ContainerEnv[parts[1]]
				if value == "" && len(parts) >= 3 {
					value = parts[2] // default value
				}
				return value
			}
			return match
		})
	}

	// ${localWorkspaceFolder}
	if ctx != nil && ctx.LocalWorkspaceFolder != "" {
		s = localWorkspaceFolderPattern.ReplaceAllString(s, ctx.LocalWorkspaceFolder)
	}

	// ${containerWorkspaceFolder}
	if ctx != nil && ctx.ContainerWorkspaceFolder != "" {
		s = containerWorkspaceFolderPattern.ReplaceAllString(s, ctx.ContainerWorkspaceFolder)
	}

	// ${localWorkspaceFolderBasename}
	if ctx != nil && ctx.LocalWorkspaceFolder != "" {
		basename := filepath.Base(ctx.LocalWorkspaceFolder)
		s = localWorkspaceFolderBasenamePattern.ReplaceAllString(s, basename)
	}

	// ${devcontainerId}
	if ctx != nil && ctx.DevcontainerID != "" {
		s = devcontainerIdPattern.ReplaceAllString(s, ctx.DevcontainerID)
	}

	// ${pathSeparator}
	s = pathSeparatorPattern.ReplaceAllString(s, string(filepath.Separator))

	return s
}

// SubstituteConfig performs variable substitution on the entire config.
func SubstituteConfig(cfg *DevcontainerConfig, ctx *SubstitutionContext) {
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
	for i, m := range cfg.Mounts {
		cfg.Mounts[i] = Substitute(m, ctx)
	}

	// Substitute in runArgs
	for i, arg := range cfg.RunArgs {
		cfg.RunArgs[i] = Substitute(arg, ctx)
	}
}

// DetermineContainerWorkspaceFolder computes the container workspace folder.
func DetermineContainerWorkspaceFolder(cfg *DevcontainerConfig, localWorkspace string) string {
	if cfg.WorkspaceFolder != "" {
		return cfg.WorkspaceFolder
	}

	// Default to /workspaces/<basename>
	basename := filepath.Base(localWorkspace)
	return "/workspaces/" + basename
}

// ResolveRelativePath resolves a path relative to a base path.
func ResolveRelativePath(base, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(base, path)
}

// NormalizeMountPath normalizes a mount path for the current platform.
func NormalizeMountPath(path string) string {
	// Clean the path
	path = filepath.Clean(path)

	// On Windows, convert backslashes to forward slashes for Docker
	if os.PathSeparator == '\\' {
		path = strings.ReplaceAll(path, "\\", "/")
	}

	return path
}
