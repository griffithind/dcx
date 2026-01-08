// Package errors provides structured error handling for dcx.
package errors

import (
	"errors"
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
	CodeConfigNotFound       = "CONFIG_NOT_FOUND"
	CodeConfigInvalid        = "CONFIG_INVALID"
	CodeConfigParse          = "CONFIG_PARSE"
	CodeConfigValidation     = "CONFIG_VALIDATION"
	CodeConfigMissing        = "CONFIG_MISSING"
	CodeConfigUnsupported    = "CONFIG_UNSUPPORTED"

	// Docker errors
	CodeDockerNotRunning     = "DOCKER_NOT_RUNNING"
	CodeDockerConnect        = "DOCKER_CONNECT"
	CodeDockerAPI            = "DOCKER_API"
	CodeDockerImage          = "DOCKER_IMAGE"
	CodeDockerContainer      = "DOCKER_CONTAINER"
	CodeDockerVolume         = "DOCKER_VOLUME"
	CodeDockerNetwork        = "DOCKER_NETWORK"

	// Feature errors
	CodeFeatureNotFound      = "FEATURE_NOT_FOUND"
	CodeFeatureResolve       = "FEATURE_RESOLVE"
	CodeFeatureInstall       = "FEATURE_INSTALL"
	CodeFeatureDependency    = "FEATURE_DEPENDENCY"
	CodeFeatureCycle         = "FEATURE_CYCLE"
	CodeFeatureInvalid       = "FEATURE_INVALID"

	// Lifecycle errors
	CodeLifecycleHook        = "LIFECYCLE_HOOK"
	CodeLifecycleTimeout     = "LIFECYCLE_TIMEOUT"
	CodeLifecycleFailed      = "LIFECYCLE_FAILED"

	// Build errors
	CodeBuildFailed          = "BUILD_FAILED"
	CodeBuildContext         = "BUILD_CONTEXT"
	CodeBuildDockerfile      = "BUILD_DOCKERFILE"

	// Compose errors
	CodeComposeNotFound      = "COMPOSE_NOT_FOUND"
	CodeComposeInvalid       = "COMPOSE_INVALID"
	CodeComposeService       = "COMPOSE_SERVICE"

	// OCI errors
	CodeOCIRegistry          = "OCI_REGISTRY"
	CodeOCIPull              = "OCI_PULL"
	CodeOCIPush              = "OCI_PUSH"
	CodeOCIAuth              = "OCI_AUTH"

	// IO errors
	CodeFileNotFound         = "FILE_NOT_FOUND"
	CodeFileRead             = "FILE_READ"
	CodeFileWrite            = "FILE_WRITE"
	CodeDirNotFound          = "DIR_NOT_FOUND"

	// Internal errors
	CodeInternal             = "INTERNAL"
	CodeNotImplemented       = "NOT_IMPLEMENTED"
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

// New creates a new DCXError.
func New(category Category, code string, message string) *DCXError {
	return &DCXError{
		Category: category,
		Code:     code,
		Message:  message,
		Context:  make(map[string]string),
	}
}

// Newf creates a new DCXError with formatted message.
func Newf(category Category, code string, format string, args ...interface{}) *DCXError {
	return &DCXError{
		Category: category,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
		Context:  make(map[string]string),
	}
}

// Wrap wraps an existing error as a DCXError.
func Wrap(err error, category Category, code string, message string) *DCXError {
	return &DCXError{
		Category: category,
		Code:     code,
		Message:  message,
		Cause:    err,
		Context:  make(map[string]string),
	}
}

// Wrapf wraps an existing error with a formatted message.
func Wrapf(err error, category Category, code string, format string, args ...interface{}) *DCXError {
	return &DCXError{
		Category: category,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
		Cause:    err,
		Context:  make(map[string]string),
	}
}

// Is checks if the error is a DCXError with the given code.
func Is(err error, code string) bool {
	var dcxErr *DCXError
	if errors.As(err, &dcxErr) {
		return dcxErr.Code == code
	}
	return false
}

// GetCategory returns the category of a DCXError, or empty string if not a DCXError.
func GetCategory(err error) Category {
	var dcxErr *DCXError
	if errors.As(err, &dcxErr) {
		return dcxErr.Category
	}
	return ""
}

// GetCode returns the code of a DCXError, or empty string if not a DCXError.
func GetCode(err error) string {
	var dcxErr *DCXError
	if errors.As(err, &dcxErr) {
		return dcxErr.Code
	}
	return ""
}

// AsDCXError attempts to convert an error to a DCXError.
func AsDCXError(err error) (*DCXError, bool) {
	var dcxErr *DCXError
	if errors.As(err, &dcxErr) {
		return dcxErr, true
	}
	return nil, false
}

// Common pre-defined errors.
var (
	// Config errors
	ErrConfigNotFound = &DCXError{
		Category: CategoryConfig,
		Code:     CodeConfigNotFound,
		Message:  "devcontainer.json not found",
		Hint:     "Create a devcontainer.json file in .devcontainer/ directory or run from a directory containing one",
		DocURL:   "https://containers.dev/implementors/json_reference/",
	}

	ErrConfigInvalid = &DCXError{
		Category: CategoryConfig,
		Code:     CodeConfigInvalid,
		Message:  "devcontainer.json is invalid",
		Hint:     "Check the JSON syntax and ensure all required fields are present",
		DocURL:   "https://containers.dev/implementors/json_reference/",
	}

	// Docker errors
	ErrDockerNotRunning = &DCXError{
		Category: CategoryDocker,
		Code:     CodeDockerNotRunning,
		Message:  "Docker daemon is not running",
		Hint:     "Start Docker Desktop or the Docker daemon service",
	}

	ErrDockerConnect = &DCXError{
		Category: CategoryDocker,
		Code:     CodeDockerConnect,
		Message:  "Failed to connect to Docker",
		Hint:     "Ensure Docker is running and you have permission to access the Docker socket",
	}

	// Feature errors
	ErrFeatureNotFound = &DCXError{
		Category: CategoryFeatures,
		Code:     CodeFeatureNotFound,
		Message:  "Feature not found",
		Hint:     "Check the feature reference and ensure it exists in the registry",
		DocURL:   "https://containers.dev/features",
	}

	ErrFeatureCycle = &DCXError{
		Category: CategoryFeatures,
		Code:     CodeFeatureCycle,
		Message:  "Circular dependency detected in features",
		Hint:     "Review feature dependencies and remove the cycle",
	}

	// Compose errors
	ErrComposeNotFound = &DCXError{
		Category: CategoryCompose,
		Code:     CodeComposeNotFound,
		Message:  "docker-compose.yml not found",
		Hint:     "Ensure the dockerComposeFile path in devcontainer.json is correct",
	}

	ErrComposeServiceNotFound = &DCXError{
		Category: CategoryCompose,
		Code:     CodeComposeService,
		Message:  "Service not found in docker-compose.yml",
		Hint:     "Ensure the service name in devcontainer.json matches a service in docker-compose.yml",
	}

	// Lifecycle errors
	ErrLifecycleTimeout = &DCXError{
		Category: CategoryLifecycle,
		Code:     CodeLifecycleTimeout,
		Message:  "Lifecycle hook timed out",
		Hint:     "The command took too long to execute. Consider optimizing the command or increasing the timeout",
	}
)

// Clone creates a copy of the error that can be modified without affecting the original.
func (e *DCXError) Clone() *DCXError {
	clone := &DCXError{
		Category: e.Category,
		Code:     e.Code,
		Message:  e.Message,
		Cause:    e.Cause,
		Hint:     e.Hint,
		DocURL:   e.DocURL,
		Context:  make(map[string]string),
	}
	for k, v := range e.Context {
		clone.Context[k] = v
	}
	return clone
}

// Config errors constructors.

// ConfigNotFound creates a config not found error.
func ConfigNotFound(path string) *DCXError {
	return ErrConfigNotFound.Clone().WithContext("path", path)
}

// ConfigInvalid creates a config invalid error.
func ConfigInvalid(path string, cause error) *DCXError {
	return ErrConfigInvalid.Clone().WithCause(cause).WithContext("path", path)
}

// ConfigParse creates a config parse error.
func ConfigParse(path string, cause error) *DCXError {
	return Wrap(cause, CategoryConfig, CodeConfigParse, "failed to parse configuration").
		WithContext("path", path).
		WithHint("Check for JSON syntax errors in the configuration file")
}

// ConfigValidation creates a validation error.
func ConfigValidation(message string) *DCXError {
	return New(CategoryConfig, CodeConfigValidation, message).
		WithHint("Review the devcontainer.json specification")
}

// Docker errors constructors.

// DockerNotRunning creates a docker not running error.
func DockerNotRunning(cause error) *DCXError {
	return ErrDockerNotRunning.Clone().WithCause(cause)
}

// DockerAPI creates a docker API error.
func DockerAPI(operation string, cause error) *DCXError {
	return Wrap(cause, CategoryDocker, CodeDockerAPI, fmt.Sprintf("Docker API error during %s", operation))
}

// DockerImage creates a docker image error.
func DockerImage(image string, cause error) *DCXError {
	return Wrap(cause, CategoryDocker, CodeDockerImage, fmt.Sprintf("failed to pull image %s", image)).
		WithContext("image", image).
		WithHint("Check that the image exists and you have permission to pull it")
}

// DockerContainer creates a docker container error.
func DockerContainer(container string, operation string, cause error) *DCXError {
	return Wrap(cause, CategoryDocker, CodeDockerContainer, fmt.Sprintf("container %s: %s failed", container, operation)).
		WithContext("container", container).
		WithContext("operation", operation)
}

// Feature errors constructors.

// FeatureNotFound creates a feature not found error.
func FeatureNotFound(feature string) *DCXError {
	return ErrFeatureNotFound.Clone().
		WithContext("feature", feature).
		WithHint(fmt.Sprintf("Check that feature %q exists and the reference is correct", feature))
}

// FeatureResolve creates a feature resolve error.
func FeatureResolve(feature string, cause error) *DCXError {
	return Wrap(cause, CategoryFeatures, CodeFeatureResolve, fmt.Sprintf("failed to resolve feature %s", feature)).
		WithContext("feature", feature)
}

// FeatureInstall creates a feature install error.
func FeatureInstall(feature string, cause error) *DCXError {
	return Wrap(cause, CategoryFeatures, CodeFeatureInstall, fmt.Sprintf("failed to install feature %s", feature)).
		WithContext("feature", feature).
		WithHint("Check the feature logs for more details")
}

// FeatureDependency creates a feature dependency error.
func FeatureDependency(feature string, dependency string) *DCXError {
	return Newf(CategoryFeatures, CodeFeatureDependency, "feature %s requires dependency %s", feature, dependency).
		WithContext("feature", feature).
		WithContext("dependency", dependency).
		WithHint("Add the missing dependency to your features list")
}

// FeatureCycle creates a feature cycle error.
func FeatureCycle(features []string) *DCXError {
	return ErrFeatureCycle.Clone().
		WithContext("cycle", strings.Join(features, " -> "))
}

// Lifecycle errors constructors.

// LifecycleHook creates a lifecycle hook error.
func LifecycleHook(hook string, cause error) *DCXError {
	return Wrap(cause, CategoryLifecycle, CodeLifecycleHook, fmt.Sprintf("%s hook failed", hook)).
		WithContext("hook", hook)
}

// LifecycleTimeout creates a lifecycle timeout error.
func LifecycleTimeout(hook string, timeout string) *DCXError {
	return ErrLifecycleTimeout.Clone().
		WithContext("hook", hook).
		WithContext("timeout", timeout)
}

// Build errors constructors.

// BuildFailed creates a build failed error.
func BuildFailed(cause error) *DCXError {
	return Wrap(cause, CategoryBuild, CodeBuildFailed, "image build failed").
		WithHint("Check the build output for errors")
}

// BuildDockerfile creates a dockerfile build error.
func BuildDockerfile(dockerfile string, cause error) *DCXError {
	return Wrap(cause, CategoryBuild, CodeBuildDockerfile, "failed to build Dockerfile").
		WithContext("dockerfile", dockerfile)
}

// Compose errors constructors.

// ComposeNotFound creates a compose file not found error.
func ComposeNotFound(path string) *DCXError {
	return ErrComposeNotFound.Clone().WithContext("path", path)
}

// ComposeService creates a compose service error.
func ComposeService(service string, cause error) *DCXError {
	return Wrap(cause, CategoryCompose, CodeComposeService, fmt.Sprintf("service %s error", service)).
		WithContext("service", service)
}

// OCI errors constructors.

// OCIRegistry creates an OCI registry error.
func OCIRegistry(registry string, cause error) *DCXError {
	return Wrap(cause, CategoryOCI, CodeOCIRegistry, fmt.Sprintf("registry %s error", registry)).
		WithContext("registry", registry)
}

// OCIPull creates an OCI pull error.
func OCIPull(reference string, cause error) *DCXError {
	return Wrap(cause, CategoryOCI, CodeOCIPull, fmt.Sprintf("failed to pull %s", reference)).
		WithContext("reference", reference).
		WithHint("Check network connectivity and that the artifact exists")
}

// OCIAuth creates an OCI auth error.
func OCIAuth(registry string, cause error) *DCXError {
	return Wrap(cause, CategoryOCI, CodeOCIAuth, fmt.Sprintf("authentication failed for %s", registry)).
		WithContext("registry", registry).
		WithHint("Ensure you are logged in to the registry (docker login)")
}

// IO errors constructors.

// FileNotFound creates a file not found error.
func FileNotFound(path string) *DCXError {
	return Newf(CategoryIO, CodeFileNotFound, "file not found: %s", path).
		WithContext("path", path)
}

// FileRead creates a file read error.
func FileRead(path string, cause error) *DCXError {
	return Wrap(cause, CategoryIO, CodeFileRead, fmt.Sprintf("failed to read file: %s", path)).
		WithContext("path", path)
}

// FileWrite creates a file write error.
func FileWrite(path string, cause error) *DCXError {
	return Wrap(cause, CategoryIO, CodeFileWrite, fmt.Sprintf("failed to write file: %s", path)).
		WithContext("path", path)
}

// Internal errors constructors.

// Internal creates an internal error.
func Internal(message string, cause error) *DCXError {
	return Wrap(cause, CategoryInternal, CodeInternal, message).
		WithHint("This is an internal error. Please report it at https://github.com/griffithind/dcx/issues")
}

// NotImplemented creates a not implemented error.
func NotImplemented(feature string) *DCXError {
	return Newf(CategoryInternal, CodeNotImplemented, "feature not implemented: %s", feature).
		WithContext("feature", feature)
}
