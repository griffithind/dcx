package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMemorySize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		// Empty and whitespace
		{"empty", "", 0},
		{"whitespace only", "   ", 0},

		// Plain bytes
		{"bytes", "1024", 1024},
		{"bytes with spaces", "  1024  ", 1024},

		// Kilobytes
		{"kilobytes lower", "1k", 1024},
		{"kilobytes upper", "1K", 1024},
		{"kilobytes with b", "1kb", 1024},
		{"kilobytes with B", "1KB", 1024},
		{"multiple kilobytes", "4k", 4 * 1024},

		// Megabytes
		{"megabytes lower", "64m", 64 * 1024 * 1024},
		{"megabytes upper", "64M", 64 * 1024 * 1024},
		{"megabytes with b", "64mb", 64 * 1024 * 1024},
		{"megabytes with B", "64MB", 64 * 1024 * 1024},
		{"512 megabytes", "512m", 512 * 1024 * 1024},

		// Gigabytes
		{"gigabytes lower", "1g", 1024 * 1024 * 1024},
		{"gigabytes upper", "1G", 1024 * 1024 * 1024},
		{"gigabytes with b", "1gb", 1024 * 1024 * 1024},
		{"gigabytes with B", "1GB", 1024 * 1024 * 1024},
		{"4 gigabytes", "4g", 4 * 1024 * 1024 * 1024},
		{"4 gigabytes with b", "4gb", 4 * 1024 * 1024 * 1024},

		// Terabytes
		{"terabytes lower", "1t", 1024 * 1024 * 1024 * 1024},
		{"terabytes upper", "1T", 1024 * 1024 * 1024 * 1024},
		{"terabytes with b", "1tb", 1024 * 1024 * 1024 * 1024},
		{"terabytes with B", "1TB", 1024 * 1024 * 1024 * 1024},

		// Float values
		{"float gigabytes", "1.5g", int64(1.5 * 1024 * 1024 * 1024)},
		{"float megabytes", "2.5m", int64(2.5 * 1024 * 1024)},
		{"float with b suffix", "1.5gb", int64(1.5 * 1024 * 1024 * 1024)},

		// Just 'b' suffix (bytes)
		{"bytes with b", "1024b", 1024},
		{"bytes with B", "1024B", 1024},

		// Invalid (returns 0)
		{"invalid format", "abc", 0},
		{"invalid unit", "100x", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseMemorySize(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseMemorySizeWithError(t *testing.T) {
	// Test valid cases return no error
	t.Run("valid cases return no error", func(t *testing.T) {
		validCases := []string{"1024", "1k", "64m", "2g", "1t", "1.5g", "4gb"}
		for _, input := range validCases {
			result, err := ParseMemorySizeWithError(input)
			require.NoError(t, err, "input: %s", input)
			assert.Greater(t, result, int64(0), "input: %s", input)
		}
	})

	// Test error cases
	t.Run("empty string returns error", func(t *testing.T) {
		_, err := ParseMemorySizeWithError("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("invalid format returns error", func(t *testing.T) {
		_, err := ParseMemorySizeWithError("abc")
		assert.Error(t, err)
	})

	t.Run("invalid unit returns error", func(t *testing.T) {
		_, err := ParseMemorySizeWithError("100xyz")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid unit")
	})
}

func TestParseShmSizeAlias(t *testing.T) {
	// Verify ParseShmSize is an alias for ParseMemorySize
	testCases := []string{"", "1024", "1k", "64m", "1g", "1.5g"}
	for _, input := range testCases {
		expected := ParseMemorySize(input)
		actual := ParseShmSize(input)
		assert.Equal(t, expected, actual, "ParseShmSize should match ParseMemorySize for input: %s", input)
	}
}
