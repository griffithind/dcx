package config

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseMemoryString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint64
		wantErr  bool
	}{
		{
			name:     "bytes",
			input:    "1024",
			expected: 1024,
		},
		{
			name:     "kilobytes",
			input:    "1kb",
			expected: 1024,
		},
		{
			name:     "megabytes",
			input:    "4mb",
			expected: 4 * 1024 * 1024,
		},
		{
			name:     "gigabytes",
			input:    "8gb",
			expected: 8 * 1024 * 1024 * 1024,
		},
		{
			name:     "gigabytes short form",
			input:    "8g",
			expected: 8 * 1024 * 1024 * 1024,
		},
		{
			name:     "terabytes",
			input:    "1tb",
			expected: 1 * 1024 * 1024 * 1024 * 1024,
		},
		{
			name:     "with spaces",
			input:    "  4 gb  ",
			expected: 4 * 1024 * 1024 * 1024,
		},
		{
			name:     "uppercase",
			input:    "4GB",
			expected: 4 * 1024 * 1024 * 1024,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseMemoryString(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes    uint64
		expected string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{8 * 1024 * 1024 * 1024, "8.0 GB"},
	}

	for _, tt := range tests {
		result := formatBytes(tt.bytes)
		assert.Equal(t, tt.expected, result)
	}
}

func TestValidateHostRequirements(t *testing.T) {
	t.Run("nil requirements", func(t *testing.T) {
		result := ValidateHostRequirements(nil)
		assert.True(t, result.Satisfied)
		assert.Empty(t, result.Errors)
		assert.Empty(t, result.Warnings)
	})

	t.Run("empty requirements", func(t *testing.T) {
		result := ValidateHostRequirements(&HostRequirements{})
		assert.True(t, result.Satisfied)
		assert.Empty(t, result.Errors)
		assert.Empty(t, result.Warnings)
	})

	t.Run("CPU requirement satisfied", func(t *testing.T) {
		result := ValidateHostRequirements(&HostRequirements{
			CPUs: 1, // Should always have at least 1 CPU
		})
		assert.True(t, result.Satisfied)
		assert.Empty(t, result.Errors)
	})

	t.Run("CPU requirement not satisfied", func(t *testing.T) {
		result := ValidateHostRequirements(&HostRequirements{
			CPUs: 1000, // Extremely high, should fail
		})
		assert.False(t, result.Satisfied)
		assert.NotEmpty(t, result.Errors)
		assert.Contains(t, result.Errors[0], "CPU requirement not met")
	})

	t.Run("memory requirement check", func(t *testing.T) {
		// This should work on any system with at least 512MB
		result := ValidateHostRequirements(&HostRequirements{
			Memory: "512mb",
		})
		// On most systems this should pass, but we just verify no crash
		assert.NotNil(t, result)
	})

	t.Run("storage warning", func(t *testing.T) {
		result := ValidateHostRequirements(&HostRequirements{
			Storage: "50gb",
		})
		assert.True(t, result.Satisfied) // Storage doesn't fail
		assert.NotEmpty(t, result.Warnings)
		assert.Contains(t, result.Warnings[0], "cannot be validated")
	})

	t.Run("GPU optional", func(t *testing.T) {
		result := ValidateHostRequirements(&HostRequirements{
			GPU: "optional",
		})
		// Should not fail, but may have warning if no GPU
		assert.True(t, result.Satisfied)
	})

	t.Run("invalid memory format", func(t *testing.T) {
		result := ValidateHostRequirements(&HostRequirements{
			Memory: "invalid",
		})
		// Should warn about invalid format
		assert.NotEmpty(t, result.Warnings)
	})
}

func TestGetHostMemory(t *testing.T) {
	mem := getHostMemory()
	// Just verify it returns a reasonable value on supported platforms
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		assert.Greater(t, mem, uint64(0), "should detect memory on %s", runtime.GOOS)
	}
}
