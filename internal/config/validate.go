package config

import (
	"fmt"
	"os"
	"strings"
)

// ValidationError represents a configuration validation error.
type ValidationError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("%s: %s", e.Field, e.Message)
	}
	return e.Message
}

// ValidationErrors is a collection of validation errors.
type ValidationErrors []ValidationError

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	if len(e) == 0 {
		return ""
	}
	if len(e) == 1 {
		return e[0].Error()
	}

	var msgs []string
	for _, err := range e {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// Validate validates a devcontainer configuration.
func Validate(cfg *DevcontainerConfig) error {
	var errs ValidationErrors

	// Must have exactly one of: image, build, or dockerComposeFile
	hasImage := cfg.Image != ""
	hasBuild := cfg.Build != nil
	hasCompose := cfg.DockerComposeFile != nil

	count := 0
	if hasImage {
		count++
	}
	if hasBuild {
		count++
	}
	if hasCompose {
		count++
	}

	if count == 0 {
		errs = append(errs, ValidationError{
			Message: "must specify one of: image, build, or dockerComposeFile",
		})
	} else if count > 1 {
		errs = append(errs, ValidationError{
			Message: "cannot specify both image/build and dockerComposeFile",
		})
	}

	// Compose-specific validation
	if hasCompose {
		files := cfg.GetDockerComposeFiles()
		if len(files) == 0 {
			errs = append(errs, ValidationError{
				Field:   "dockerComposeFile",
				Message: "must specify at least one compose file",
			})
		}

		if cfg.Service == "" {
			errs = append(errs, ValidationError{
				Field:   "service",
				Message: "must specify service when using dockerComposeFile",
			})
		}
	}

	// Build-specific validation
	if hasBuild {
		if cfg.Build.Dockerfile == "" && cfg.Build.Context == "" {
			errs = append(errs, ValidationError{
				Field:   "build",
				Message: "must specify dockerfile or context",
			})
		}
	}

	// Workspace folder validation
	if cfg.WorkspaceFolder != "" && !isAbsolutePath(cfg.WorkspaceFolder) {
		errs = append(errs, ValidationError{
			Field:   "workspaceFolder",
			Message: "must be an absolute path",
		})
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}

// isAbsolutePath checks if a path is absolute.
func isAbsolutePath(path string) bool {
	// Unix absolute path
	if strings.HasPrefix(path, "/") {
		return true
	}
	// Windows absolute path (C:\, D:\, etc.)
	if len(path) >= 3 && path[1] == ':' && (path[2] == '/' || path[2] == '\\') {
		return true
	}
	return false
}

// ValidateForCompose validates compose-specific configuration.
func ValidateForCompose(cfg *DevcontainerConfig, configDir string) error {
	var errs ValidationErrors

	if !cfg.IsComposePlan() {
		return nil
	}

	// Check that compose files exist
	files := cfg.GetDockerComposeFiles()
	for _, f := range files {
		path := ResolveRelativePath(configDir, f)
		if _, err := os.Stat(path); err != nil {
			errs = append(errs, ValidationError{
				Field:   "dockerComposeFile",
				Message: fmt.Sprintf("file not found: %s", f),
			})
		}
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}

// ValidateForBuild validates build-specific configuration.
func ValidateForBuild(cfg *DevcontainerConfig, configDir string) error {
	var errs ValidationErrors

	if cfg.Build == nil {
		return nil
	}

	// Check Dockerfile exists
	if cfg.Build.Dockerfile != "" {
		path := ResolveRelativePath(configDir, cfg.Build.Dockerfile)
		if _, err := os.Stat(path); err != nil {
			errs = append(errs, ValidationError{
				Field:   "build.dockerfile",
				Message: fmt.Sprintf("file not found: %s", cfg.Build.Dockerfile),
			})
		}
	}

	// Check context directory exists
	if cfg.Build.Context != "" {
		path := ResolveRelativePath(configDir, cfg.Build.Context)
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			errs = append(errs, ValidationError{
				Field:   "build.context",
				Message: fmt.Sprintf("directory not found: %s", cfg.Build.Context),
			})
		}
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}
