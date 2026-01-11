package devcontainer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMemoryString(t *testing.T) {
	// Test valid cases return no error
	t.Run("valid cases return no error", func(t *testing.T) {
		validCases := []string{"1024", "1k", "64m", "2g", "1t", "1.5g", "4gb"}
		for _, input := range validCases {
			result, err := parseMemoryString(input)
			require.NoError(t, err, "input: %s", input)
			assert.Greater(t, result, uint64(0), "input: %s", input)
		}
	})

	// Test error cases
	t.Run("empty string returns error", func(t *testing.T) {
		_, err := parseMemoryString("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("invalid format returns error", func(t *testing.T) {
		_, err := parseMemoryString("abc")
		assert.Error(t, err)
	})

	t.Run("invalid unit returns error", func(t *testing.T) {
		_, err := parseMemoryString("100xyz")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid unit")
	})
}

func TestParseMemoryString_Values(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"1024", 1024},
		{"1k", 1024},
		{"1kb", 1024},
		{"1m", 1024 * 1024},
		{"1mb", 1024 * 1024},
		{"1g", 1024 * 1024 * 1024},
		{"1gb", 1024 * 1024 * 1024},
		{"2g", 2 * 1024 * 1024 * 1024},
		{"1.5g", uint64(1.5 * 1024 * 1024 * 1024)},
		{"512m", 512 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseMemoryString(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
