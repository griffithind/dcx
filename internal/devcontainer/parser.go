package devcontainer

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
)

// Standard locations for devcontainer.json
var configLocations = []string{
	".devcontainer/devcontainer.json",
	".devcontainer.json",
}

// Parse parses a devcontainer.json file from bytes (without substitution).
func Parse(data []byte) (*DevContainerConfig, error) {
	_, cfg, _, err := parseConfig(data)
	return cfg, err
}

// parseConfig parses JSONC into its generic tree (with mis-cased property
// names removed, reported as warnings) and the typed config decoded from it.
func parseConfig(data []byte) (map[string]any, *DevContainerConfig, []string, error) {
	tree, err := parseTree(data)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to parse devcontainer.json: %w", err)
	}
	obj, ok := tree.(map[string]any)
	if !ok {
		return nil, nil, nil, fmt.Errorf("failed to parse devcontainer.json: not a JSON object")
	}
	warnings := dropMiscasedKeys(obj, reflect.TypeOf(DevContainerConfig{}), "")
	cfg, err := decodeConfig(obj)
	if err != nil {
		return nil, nil, nil, err
	}
	return obj, cfg, warnings, nil
}

// decodeConfig decodes a config tree, moving the legacy top-level
// dockerFile/context into build as the reference implementation reads them:
// a top-level dockerFile wins over build.dockerfile, and then the context is
// the top-level one (build.args, target, ... still apply).
func decodeConfig(tree map[string]any) (*DevContainerConfig, error) {
	var cfg DevContainerConfig
	if err := decodeTree(tree, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse devcontainer.json: %w", err)
	}
	if cfg.LegacyDockerFile != "" {
		if cfg.Build == nil {
			cfg.Build = &BuildConfig{}
		}
		cfg.Build.Dockerfile, cfg.Build.Context = cfg.LegacyDockerFile, cfg.LegacyContext
	}
	cfg.LegacyDockerFile, cfg.LegacyContext = "", ""
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
	if !isDir(workspacePath) {
		return "", fmt.Errorf("workspace directory does not exist: %s", workspacePath)
	}

	// Try each standard location first
	for _, loc := range configLocations {
		configPath := filepath.Join(workspacePath, loc)
		if isFile(configPath) {
			return configPath, nil
		}
	}

	devcontainerDir := filepath.Join(workspacePath, ".devcontainer")
	if !isDir(devcontainerDir) {
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
			if isFile(configPath) {
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

	// Any other .devcontainer/*.json (it used to take the first one, which
	// could be an unrelated file) needs --config, as the spec defines only
	// the locations above.
	return "", fmt.Errorf("no devcontainer.json found in %s (use --config for a file elsewhere)", workspacePath)
}

// Loaded is a devcontainer.json as written and as substituted.
type Loaded struct {
	// Path is the config file.
	Path string
	// Raw is the config as written (the staleness key's input).
	Raw *DevContainerConfig
	// Config is Raw with variables substituted.
	Config *DevContainerConfig
	// Warnings are problems that did not stop loading (ignored
	// properties).
	Warnings []string
}

// Load locates, parses and substitutes the devcontainer configuration.
// Returns the config and the path it was loaded from.
func Load(workspacePath, configPath string) (*DevContainerConfig, string, error) {
	loaded, err := LoadConfig(workspacePath, configPath)
	if err != nil {
		if loaded != nil {
			return nil, loaded.Path, err
		}
		return nil, "", err
	}
	return loaded.Config, loaded.Path, nil
}

// LoadConfig locates and parses the devcontainer configuration, and
// substitutes variables in every string of it, once, whether the file was
// found automatically or given via --config.
func LoadConfig(workspacePath, configPath string) (*Loaded, error) {
	if configPath != "" {
		if !filepath.IsAbs(configPath) {
			configPath = filepath.Join(workspacePath, configPath)
		}
	} else {
		resolvedPath, err := Resolve(workspacePath)
		if err != nil {
			return nil, err
		}
		configPath = resolvedPath
	}
	loaded := &Loaded{Path: configPath}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return loaded, fmt.Errorf("failed to read %s: %w", configPath, err)
	}
	tree, raw, warnings, err := parseConfig(data)
	if err != nil {
		return loaded, err
	}
	subCtx := NewSubstitutionContext(workspacePath, configPath, raw)
	cfg, err := decodeConfig(substituteTree(tree, subCtx).(map[string]any))
	if err != nil {
		return loaded, err
	}
	loaded.Raw, loaded.Config, loaded.Warnings = raw, cfg, warnings
	return loaded, nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
