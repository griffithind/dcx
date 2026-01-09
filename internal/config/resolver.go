package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/util"
)

// Standard locations for devcontainer.json
var configLocations = []string{
	".devcontainer/devcontainer.json",
	".devcontainer.json",
	".devcontainer/docker-compose.yml", // compose file can serve as implicit config
}

// Resolve finds the devcontainer.json file in a workspace.
func Resolve(workspacePath string) (string, error) {
	// Ensure workspace exists
	if !util.IsDir(workspacePath) {
		return "", fmt.Errorf("workspace directory does not exist: %s", workspacePath)
	}

	// Try each standard location
	for _, loc := range configLocations {
		configPath := filepath.Join(workspacePath, loc)
		if util.IsFile(configPath) {
			return configPath, nil
		}
	}

	// Check for devcontainer directory with custom named JSON file
	devcontainerDir := filepath.Join(workspacePath, ".devcontainer")
	if util.IsDir(devcontainerDir) {
		entries, err := os.ReadDir(devcontainerDir)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
					return filepath.Join(devcontainerDir, entry.Name()), nil
				}
			}
		}
	}

	return "", fmt.Errorf("no devcontainer.json found in %s", workspacePath)
}

// Load loads and parses the devcontainer configuration.
// Returns the parsed config and the path to the config file.
func Load(workspacePath, configPath string) (*DevcontainerConfig, string, error) {
	// If config path is specified, use it
	if configPath != "" {
		if !filepath.IsAbs(configPath) {
			configPath = filepath.Join(workspacePath, configPath)
		}
		cfg, err := ParseFile(configPath)
		return cfg, configPath, err
	}

	// Otherwise, resolve the config file
	resolvedPath, err := Resolve(workspacePath)
	if err != nil {
		return nil, "", err
	}

	cfg, err := ParseFile(resolvedPath)
	if err != nil {
		return nil, resolvedPath, err
	}

	// Compute container workspace folder
	containerWorkspace := DetermineContainerWorkspaceFolder(cfg, workspacePath)

	// Create substitution context
	ctx := &SubstitutionContext{
		LocalWorkspaceFolder:     workspacePath,
		ContainerWorkspaceFolder: containerWorkspace,
	}

	// Perform variable substitution
	SubstituteConfig(cfg, ctx)

	return cfg, resolvedPath, nil
}
