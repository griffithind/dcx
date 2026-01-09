package errors

import (
	"errors"
	"strings"
	"testing"
)

func TestDCXError_Error(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "devcontainer.json not found")

	expected := "[configuration/CONFIG_NOT_FOUND] devcontainer.json not found"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestDCXError_Unwrap(t *testing.T) {
	cause := errors.New("underlying error")
	err := Wrap(cause, CategoryDocker, CodeDockerConnect, "docker error")

	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestDCXError_UserFriendly(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "devcontainer.json not found").
		WithHint("Create a config file").
		WithContext("path", "/project/.devcontainer")

	friendly := err.UserFriendly()

	if !strings.Contains(friendly, "devcontainer.json not found") {
		t.Error("should contain message")
	}
	if !strings.Contains(friendly, "Create a config file") {
		t.Error("should contain hint")
	}
	if !strings.Contains(friendly, "path: /project/.devcontainer") {
		t.Error("should contain context")
	}
}

func TestDCXError_WithCause(t *testing.T) {
	cause := errors.New("cause")
	err := New(CategoryDocker, CodeDockerConnect, "error").WithCause(cause)

	if err.Cause != cause {
		t.Error("cause not set")
	}
}

func TestDCXError_WithHint(t *testing.T) {
	err := New(CategoryDocker, CodeDockerConnect, "error").WithHint("try this")

	if err.Hint != "try this" {
		t.Errorf("hint not set, got %q", err.Hint)
	}
}

func TestDCXError_WithContext(t *testing.T) {
	err := New(CategoryDocker, CodeDockerConnect, "error").
		WithContext("key1", "value1").
		WithContext("key2", "value2")

	if err.Context["key1"] != "value1" {
		t.Error("key1 not set")
	}
	if err.Context["key2"] != "value2" {
		t.Error("key2 not set")
	}
}

func TestNew(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "not found")

	if err.Category != CategoryConfig {
		t.Errorf("wrong category: %v", err.Category)
	}
	if err.Code != CodeConfigNotFound {
		t.Errorf("wrong code: %s", err.Code)
	}
	if err.Message != "not found" {
		t.Errorf("wrong message: %s", err.Message)
	}
}

func TestNewf(t *testing.T) {
	err := Newf(CategoryConfig, CodeConfigNotFound, "file %s not found", "test.json")

	if err.Message != "file test.json not found" {
		t.Errorf("wrong message: %s", err.Message)
	}
}

func TestWrap(t *testing.T) {
	cause := errors.New("original")
	err := Wrap(cause, CategoryDocker, CodeDockerConnect, "wrapped")

	if err.Cause != cause {
		t.Error("cause not set")
	}
	if err.Message != "wrapped" {
		t.Errorf("wrong message: %s", err.Message)
	}
}

func TestWrapf(t *testing.T) {
	cause := errors.New("original")
	err := Wrapf(cause, CategoryDocker, CodeDockerConnect, "wrapped %s", "error")

	if err.Message != "wrapped error" {
		t.Errorf("wrong message: %s", err.Message)
	}
}

func TestIs(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "not found")

	if !Is(err, CodeConfigNotFound) {
		t.Error("should match code")
	}
	if Is(err, CodeConfigInvalid) {
		t.Error("should not match different code")
	}
	if Is(errors.New("other"), CodeConfigNotFound) {
		t.Error("should not match non-DCXError")
	}
}

func TestErrorsAs(t *testing.T) {
	// Test that DCXError works with errors.As
	dcxErr := New(CategoryConfig, CodeConfigNotFound, "not found")

	// A proper wrap should work
	err := Wrap(dcxErr, CategoryDocker, CodeDockerConnect, "higher level error")

	var target *DCXError
	if !errors.As(err, &target) {
		t.Error("should be able to extract DCXError with errors.As")
	}
}
