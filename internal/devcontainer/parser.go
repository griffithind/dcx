package devcontainer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/util"
	"github.com/tidwall/jsonc"
)

// Standard locations for devcontainer.json
var configLocations = []string{
	".devcontainer/devcontainer.json",
	".devcontainer.json",
}

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

// Resolve finds the devcontainer.json file in a workspace.
// Per spec, supports multi-folder configurations: .devcontainer/<folder>/devcontainer.json
func Resolve(workspacePath string) (string, error) {
	// Ensure workspace exists
	if !util.IsDir(workspacePath) {
		return "", fmt.Errorf("workspace directory does not exist: %s", workspacePath)
	}

	// Try each standard location first
	for _, loc := range configLocations {
		configPath := filepath.Join(workspacePath, loc)
		if util.IsFile(configPath) {
			return configPath, nil
		}
	}

	devcontainerDir := filepath.Join(workspacePath, ".devcontainer")
	if !util.IsDir(devcontainerDir) {
		return "", fmt.Errorf("no devcontainer.json found in %s", workspacePath)
	}

	entries, err := os.ReadDir(devcontainerDir)
	if err != nil {
		return "", fmt.Errorf("failed to read .devcontainer directory: %w", err)
	}

	// Check for multi-folder configurations: .devcontainer/<folder>/devcontainer.json
	var folderConfigs []string
	var folderNames []string
	for _, entry := range entries {
		if entry.IsDir() {
			configPath := filepath.Join(devcontainerDir, entry.Name(), "devcontainer.json")
			if util.IsFile(configPath) {
				folderConfigs = append(folderConfigs, configPath)
				folderNames = append(folderNames, entry.Name())
			}
		}
	}

	// If multiple folder configurations found, return error listing options
	if len(folderConfigs) > 1 {
		return "", fmt.Errorf("multiple devcontainer configurations found: %v. Use --config to select one (e.g., --config .devcontainer/%s/devcontainer.json)", folderNames, folderNames[0])
	}

	// If exactly one folder config, use it
	if len(folderConfigs) == 1 {
		return folderConfigs[0], nil
	}

	// Fall back to custom named JSON file in .devcontainer directory
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			return filepath.Join(devcontainerDir, entry.Name()), nil
		}
	}

	return "", fmt.Errorf("no devcontainer.json found in %s", workspacePath)
}

// Load locates, parses and substitutes the devcontainer configuration.
// Returns the config and the path it was loaded from. Substitution runs
// exactly once here, whether the file was found automatically or given via
// --config.
func Load(workspacePath, configPath string) (*DevContainerConfig, string, error) {
	if configPath != "" {
		if !filepath.IsAbs(configPath) {
			configPath = filepath.Join(workspacePath, configPath)
		}
	} else {
		resolvedPath, err := Resolve(workspacePath)
		if err != nil {
			return nil, "", err
		}
		configPath = resolvedPath
	}

	cfg, err := ParseFile(configPath)
	if err != nil {
		return nil, configPath, err
	}

	SubstituteConfig(cfg, NewSubstitutionContext(workspacePath, cfg))
	return cfg, configPath, nil
}
