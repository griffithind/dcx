package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
