package devcontainer

import (
	"fmt"
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
func Validate(cfg *DevContainerConfig) error {
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
