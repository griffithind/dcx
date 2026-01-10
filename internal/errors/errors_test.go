package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDCXError_Error(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "devcontainer.json not found")

	expected := "[configuration/CONFIG_NOT_FOUND] devcontainer.json not found"
	assert.Equal(t, expected, err.Error())
}

func TestDCXError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := Wrap(cause, CategoryDocker, CodeDockerConnect, "docker error")

	assert.Equal(t, cause, err.Unwrap(), "Unwrap should return the cause")
}

func TestDCXError_UserFriendly(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "devcontainer.json not found").
		WithHint("Create a config file").
		WithContext("path", "/project/.devcontainer")

	friendly := err.UserFriendly()

	assert.Contains(t, friendly, "devcontainer.json not found", "should contain message")
	assert.Contains(t, friendly, "Create a config file", "should contain hint")
	assert.Contains(t, friendly, "path: /project/.devcontainer", "should contain context")
}

func TestDCXError_WithCause(t *testing.T) {
	cause := errors.New("cause")
	err := New(CategoryDocker, CodeDockerConnect, "error").WithCause(cause)

	assert.Equal(t, cause, err.Cause, "cause should be set")
}

func TestDCXError_WithHint(t *testing.T) {
	err := New(CategoryDocker, CodeDockerConnect, "error").WithHint("try this")

	assert.Equal(t, "try this", err.Hint)
}

func TestDCXError_WithContext(t *testing.T) {
	err := New(CategoryDocker, CodeDockerConnect, "error").
		WithContext("key1", "value1").
		WithContext("key2", "value2")

	assert.Equal(t, "value1", err.Context["key1"])
	assert.Equal(t, "value2", err.Context["key2"])
}

func TestNew(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "not found")

	assert.Equal(t, CategoryConfig, err.Category)
	assert.Equal(t, CodeConfigNotFound, err.Code)
	assert.Equal(t, "not found", err.Message)
}

func TestNewf(t *testing.T) {
	err := Newf(CategoryConfig, CodeConfigNotFound, "file %s not found", "test.json")

	assert.Equal(t, "file test.json not found", err.Message)
}

func TestWrap(t *testing.T) {
	cause := errors.New("original")
	err := Wrap(cause, CategoryDocker, CodeDockerConnect, "wrapped")

	assert.Equal(t, cause, err.Cause, "cause should be set")
	assert.Equal(t, "wrapped", err.Message)
}

func TestWrapf(t *testing.T) {
	cause := errors.New("original")
	err := Wrapf(cause, CategoryDocker, CodeDockerConnect, "wrapped %s", "error")

	assert.Equal(t, "wrapped error", err.Message)
}

func TestIs(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "not found")

	assert.True(t, Is(err, CodeConfigNotFound), "should match code")
	assert.False(t, Is(err, CodeConfigInvalid), "should not match different code")
	assert.False(t, Is(errors.New("other"), CodeConfigNotFound), "should not match non-DCXError")
}

func TestErrorsAs(t *testing.T) {
	// Test that DCXError works with errors.As
	dcxErr := New(CategoryConfig, CodeConfigNotFound, "not found")

	// A proper wrap should work
	err := Wrap(dcxErr, CategoryDocker, CodeDockerConnect, "higher level error")

	var target *DCXError
	require.True(t, errors.As(err, &target), "should be able to extract DCXError with errors.As")
}
