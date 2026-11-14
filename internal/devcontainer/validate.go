package devcontainer

import (
	"fmt"
	"sort"
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

	// One of image, build or dockerComposeFile. image and build together
	// are fine: the image is built and also tagged as image (or, for a
	// build without a dockerfile, the image is used), as in the reference
	// implementation. Compose with either is ambiguous.
	hasImage := cfg.Image != ""
	hasBuild := cfg.Build != nil
	hasCompose := cfg.DockerComposeFile != nil

	switch {
	case !hasImage && !hasBuild && !hasCompose:
		errs = append(errs, ValidationError{
			Message: "must specify one of: image, build, or dockerComposeFile",
		})
	case hasCompose && (hasImage || hasBuild):
		set := []string{}
		for name, ok := range map[string]bool{"image": hasImage, "build": hasBuild, "dockerComposeFile": hasCompose} {
			if ok {
				set = append(set, name)
			}
		}
		sort.Strings(set)
		errs = append(errs, ValidationError{
			Message: "dockerComposeFile cannot be combined with image or build (found " + strings.Join(set, ", ") + ")",
		})
	}

	// Compose-specific validation
	if hasCompose {
		files := cfg.DockerComposeFile
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
	if cfg.PlanType() == PlanTypeDockerfile {
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

	if cfg.WorkspaceMount != nil && *cfg.WorkspaceMount != "" {
		if _, err := ParseWorkspaceMount(*cfg.WorkspaceMount); err != nil {
			errs = append(errs, ValidationError{Field: "workspaceMount", Message: err.Error()})
		}
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
