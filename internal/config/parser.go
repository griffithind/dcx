package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tidwall/jsonc"
)

// Parse parses a devcontainer.json file from bytes.
func Parse(data []byte) (*DevContainerConfig, error) {
	// Strip comments and trailing commas
	stripped := jsonc.ToJSON(data)

	var cfg DevContainerConfig
	if err := json.Unmarshal(stripped, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse devcontainer.json: %w", err)
	}

	// Store raw JSON for hash computation
	cfg.SetRawJSON(stripped)

	return &cfg, nil
}

// ParseFile parses a devcontainer.json file from a path.
func ParseFile(path string) (*DevContainerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	return Parse(data)
}

// SubstitutionContext provides values for variable substitution.
type SubstitutionContext struct {
	LocalWorkspaceFolder     string
	ContainerWorkspaceFolder string
	DevcontainerID           string
	UserHome                 string                 // User's home directory for ${userHome}
	ContainerEnv             map[string]string      // Container environment variables for ${containerEnv:VAR}
	LocalEnv                 func(string) string    // Optional function to get local env vars; falls back to os.Getenv
}

// Substitute performs variable substitution on a string.
// It delegates to the registry-based implementation in substitute.go.
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
func DetermineContainerWorkspaceFolder(cfg *DevContainerConfig, localWorkspace string) string {
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
