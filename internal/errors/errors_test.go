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
	err := Wrap(cause, CategoryDocker, CodeDockerAPI, "docker error")

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
	err := New(CategoryDocker, CodeDockerAPI, "error").WithCause(cause)

	if err.Cause != cause {
		t.Error("cause not set")
	}
}

func TestDCXError_WithHint(t *testing.T) {
	err := New(CategoryDocker, CodeDockerAPI, "error").WithHint("try this")

	if err.Hint != "try this" {
		t.Errorf("hint not set, got %q", err.Hint)
	}
}

func TestDCXError_WithContext(t *testing.T) {
	err := New(CategoryDocker, CodeDockerAPI, "error").
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
	err := Wrap(cause, CategoryDocker, CodeDockerAPI, "wrapped")

	if err.Cause != cause {
		t.Error("cause not set")
	}
	if err.Message != "wrapped" {
		t.Errorf("wrong message: %s", err.Message)
	}
}

func TestWrapf(t *testing.T) {
	cause := errors.New("original")
	err := Wrapf(cause, CategoryDocker, CodeDockerAPI, "wrapped %s", "error")

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

func TestGetCategory(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "not found")

	if GetCategory(err) != CategoryConfig {
		t.Errorf("wrong category: %v", GetCategory(err))
	}
	if GetCategory(errors.New("other")) != "" {
		t.Error("should return empty for non-DCXError")
	}
}

func TestGetCode(t *testing.T) {
	err := New(CategoryConfig, CodeConfigNotFound, "not found")

	if GetCode(err) != CodeConfigNotFound {
		t.Errorf("wrong code: %s", GetCode(err))
	}
	if GetCode(errors.New("other")) != "" {
		t.Error("should return empty for non-DCXError")
	}
}

func TestAsDCXError(t *testing.T) {
	dcxErr := New(CategoryConfig, CodeConfigNotFound, "not found")

	result, ok := AsDCXError(dcxErr)
	if !ok {
		t.Error("should return true for DCXError")
	}
	if result != dcxErr {
		t.Error("should return the same error")
	}

	_, ok = AsDCXError(errors.New("other"))
	if ok {
		t.Error("should return false for non-DCXError")
	}
}

func TestClone(t *testing.T) {
	original := New(CategoryConfig, CodeConfigNotFound, "not found").
		WithHint("hint").
		WithContext("key", "value")

	clone := original.Clone()

	// Modify clone
	clone.Message = "modified"
	clone.Context["key"] = "modified"
	clone.Context["new"] = "new"

	// Original should be unchanged
	if original.Message != "not found" {
		t.Error("original message should not change")
	}
	if original.Context["key"] != "value" {
		t.Error("original context should not change")
	}
	if _, ok := original.Context["new"]; ok {
		t.Error("original should not have new key")
	}
}

func TestPredefinedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  *DCXError
		code string
	}{
		{"ConfigNotFound", ErrConfigNotFound, CodeConfigNotFound},
		{"ConfigInvalid", ErrConfigInvalid, CodeConfigInvalid},
		{"DockerNotRunning", ErrDockerNotRunning, CodeDockerNotRunning},
		{"DockerConnect", ErrDockerConnect, CodeDockerConnect},
		{"FeatureNotFound", ErrFeatureNotFound, CodeFeatureNotFound},
		{"FeatureCycle", ErrFeatureCycle, CodeFeatureCycle},
		{"ComposeNotFound", ErrComposeNotFound, CodeComposeNotFound},
		{"ComposeServiceNotFound", ErrComposeServiceNotFound, CodeComposeService},
		{"LifecycleTimeout", ErrLifecycleTimeout, CodeLifecycleTimeout},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err.Code != tc.code {
				t.Errorf("wrong code: expected %s, got %s", tc.code, tc.err.Code)
			}
			if tc.err.Message == "" {
				t.Error("message should not be empty")
			}
		})
	}
}

func TestConstructors(t *testing.T) {
	t.Run("ConfigNotFound", func(t *testing.T) {
		err := ConfigNotFound("/path/to/config")
		if err.Code != CodeConfigNotFound {
			t.Errorf("wrong code: %s", err.Code)
		}
		if err.Context["path"] != "/path/to/config" {
			t.Error("path context not set")
		}
	})

	t.Run("ConfigInvalid", func(t *testing.T) {
		cause := errors.New("parse error")
		err := ConfigInvalid("/path", cause)
		if err.Cause != cause {
			t.Error("cause not set")
		}
	})

	t.Run("DockerAPI", func(t *testing.T) {
		cause := errors.New("api error")
		err := DockerAPI("pull", cause)
		if !strings.Contains(err.Message, "pull") {
			t.Error("operation not in message")
		}
	})

	t.Run("FeatureNotFound", func(t *testing.T) {
		err := FeatureNotFound("ghcr.io/features/go")
		if err.Context["feature"] != "ghcr.io/features/go" {
			t.Error("feature context not set")
		}
	})

	t.Run("FeatureCycle", func(t *testing.T) {
		err := FeatureCycle([]string{"a", "b", "a"})
		if err.Context["cycle"] != "a -> b -> a" {
			t.Errorf("cycle context wrong: %s", err.Context["cycle"])
		}
	})

	t.Run("LifecycleHook", func(t *testing.T) {
		cause := errors.New("command failed")
		err := LifecycleHook("postCreate", cause)
		if err.Context["hook"] != "postCreate" {
			t.Error("hook context not set")
		}
	})

	t.Run("OCIPull", func(t *testing.T) {
		cause := errors.New("network error")
		err := OCIPull("ghcr.io/image:tag", cause)
		if err.Context["reference"] != "ghcr.io/image:tag" {
			t.Error("reference context not set")
		}
	})

	t.Run("Internal", func(t *testing.T) {
		cause := errors.New("bug")
		err := Internal("something went wrong", cause)
		if !strings.Contains(err.Hint, "report") {
			t.Error("should have report hint")
		}
	})
}

func TestErrorsAs(t *testing.T) {
	// Test that DCXError works with errors.As
	dcxErr := New(CategoryConfig, CodeConfigNotFound, "not found")
	wrappedErr := errors.New("wrapped: " + dcxErr.Error())
	_ = wrappedErr // This won't actually wrap with errors.As compatibility

	// But a proper wrap should work
	err := Wrap(dcxErr, CategoryDocker, CodeDockerAPI, "higher level error")

	var target *DCXError
	if !errors.As(err, &target) {
		t.Error("should be able to extract DCXError with errors.As")
	}
}
