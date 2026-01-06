// Package util provides shared utilities for dcx.
package util

import (
	"errors"
	"fmt"
)

// ErrorCode represents a specific error category.
type ErrorCode string

const (
	// ErrDockerNotRunning indicates Docker daemon is not accessible.
	ErrDockerNotRunning ErrorCode = "DOCKER_NOT_RUNNING"
	// ErrConfigNotFound indicates devcontainer.json was not found.
	ErrConfigNotFound ErrorCode = "CONFIG_NOT_FOUND"
	// ErrConfigInvalid indicates the configuration is invalid.
	ErrConfigInvalid ErrorCode = "CONFIG_INVALID"
	// ErrFeatureNotFound indicates a feature could not be resolved.
	ErrFeatureNotFound ErrorCode = "FEATURE_NOT_FOUND"
	// ErrNetworkRequired indicates the operation requires network access.
	ErrNetworkRequired ErrorCode = "NETWORK_REQUIRED"
	// ErrContainerNotFound indicates the expected container was not found.
	ErrContainerNotFound ErrorCode = "CONTAINER_NOT_FOUND"
	// ErrStateInconsistent indicates the container state is inconsistent.
	ErrStateInconsistent ErrorCode = "STATE_INCONSISTENT"
	// ErrComposeError indicates a docker compose operation failed.
	ErrComposeError ErrorCode = "COMPOSE_ERROR"
	// ErrSSHAgentError indicates an SSH agent operation failed.
	ErrSSHAgentError ErrorCode = "SSH_AGENT_ERROR"
)

// DCXError represents a structured error with categorization.
type DCXError struct {
	Code       ErrorCode
	Message    string
	Cause      error
	OfflineSafe bool // True if this error is acceptable in offline mode
}

// Error implements the error interface.
func (e *DCXError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the underlying cause.
func (e *DCXError) Unwrap() error {
	return e.Cause
}

// NewError creates a new DCXError.
func NewError(code ErrorCode, message string, cause error) *DCXError {
	return &DCXError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

// NewOfflineSafeError creates a new DCXError that is acceptable offline.
func NewOfflineSafeError(code ErrorCode, message string, cause error) *DCXError {
	return &DCXError{
		Code:        code,
		Message:     message,
		Cause:       cause,
		OfflineSafe: true,
	}
}

// IsCode checks if an error has the specified error code.
func IsCode(err error, code ErrorCode) bool {
	var dcxErr *DCXError
	if errors.As(err, &dcxErr) {
		return dcxErr.Code == code
	}
	return false
}

// IsOfflineSafe checks if an error is acceptable in offline mode.
func IsOfflineSafe(err error) bool {
	var dcxErr *DCXError
	if errors.As(err, &dcxErr) {
		return dcxErr.OfflineSafe
	}
	return false
}
