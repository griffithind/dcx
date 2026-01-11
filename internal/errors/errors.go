// Package errors provides structured error handling for dcx.
package errors

import (
	"fmt"
	"strings"
)

// Category represents the error category.
type Category string

// Error categories.
const (
	CategoryConfig    Category = "configuration"
	CategoryDocker    Category = "docker"
	CategoryFeatures  Category = "features"
	CategoryLifecycle Category = "lifecycle"
	CategoryNetwork   Category = "network"
	CategoryBuild     Category = "build"
	CategoryCompose   Category = "compose"
	CategoryOCI       Category = "oci"
	CategoryIO        Category = "io"
	CategoryInternal  Category = "internal"
)

// Error codes for each category.
const (
	// Config errors
	CodeConfigNotFound    = "CONFIG_NOT_FOUND"
	CodeConfigInvalid     = "CONFIG_INVALID"
	CodeConfigParse       = "CONFIG_PARSE"
	CodeConfigValidation  = "CONFIG_VALIDATION"
	CodeConfigMissing     = "CONFIG_MISSING"
	CodeConfigUnsupported = "CONFIG_UNSUPPORTED"

	// Docker errors
	CodeDockerNotRunning = "DOCKER_NOT_RUNNING"
	CodeDockerConnect    = "DOCKER_CONNECT"
	CodeDockerVolume     = "DOCKER_VOLUME"
	CodeDockerNetwork    = "DOCKER_NETWORK"

	// Feature errors
	CodeFeatureInvalid = "FEATURE_INVALID"

	// Lifecycle errors
	CodeLifecycleFailed = "LIFECYCLE_FAILED"

	// Build errors
	CodeBuildContext = "BUILD_CONTEXT"

	// Compose errors
	CodeComposeInvalid = "COMPOSE_INVALID"
)

// DCXError is a structured error with category, code, and user-friendly hints.
type DCXError struct {
	Category Category
	Code     string
	Message  string
	Cause    error
	Hint     string
	DocURL   string
	Context  map[string]string
}

// Error implements the error interface.
func (e *DCXError) Error() string {
	return fmt.Sprintf("[%s/%s] %s", e.Category, e.Code, e.Message)
}

// Unwrap returns the underlying cause.
func (e *DCXError) Unwrap() error {
	return e.Cause
}

// UserFriendly returns a user-friendly error message with hints.
func (e *DCXError) UserFriendly() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Error: %s\n", e.Message))

	if e.Cause != nil {
		sb.WriteString(fmt.Sprintf("Cause: %s\n", e.Cause.Error()))
	}

	if e.Hint != "" {
		sb.WriteString(fmt.Sprintf("\nHint: %s\n", e.Hint))
	}

	if e.DocURL != "" {
		sb.WriteString(fmt.Sprintf("\nDocumentation: %s\n", e.DocURL))
	}

	if len(e.Context) > 0 {
		sb.WriteString("\nContext:\n")
		for k, v := range e.Context {
			sb.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
		}
	}

	return sb.String()
}

// WithCause adds a cause to the error.
func (e *DCXError) WithCause(cause error) *DCXError {
	e.Cause = cause
	return e
}

// WithHint adds a hint to the error.
func (e *DCXError) WithHint(hint string) *DCXError {
	e.Hint = hint
	return e
}

// WithContext adds context to the error.
func (e *DCXError) WithContext(key, value string) *DCXError {
	if e.Context == nil {
		e.Context = make(map[string]string)
	}
	e.Context[key] = value
	return e
}
