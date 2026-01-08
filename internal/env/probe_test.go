package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProbeCommand(t *testing.T) {
	tests := []struct {
		name      string
		probeType ProbeType
		expected  []string
	}{
		{
			name:      "none returns nil",
			probeType: ProbeNone,
			expected:  nil,
		},
		{
			name:      "empty returns nil",
			probeType: "",
			expected:  nil,
		},
		{
			name:      "loginShell",
			probeType: ProbeLoginShell,
			expected:  []string{"sh", "-l", "-c", "env"},
		},
		{
			name:      "loginInteractiveShell",
			probeType: ProbeLoginInteractiveShell,
			expected:  []string{"sh", "-l", "-i", "-c", "env"},
		},
		{
			name:      "interactiveShell",
			probeType: ProbeInteractiveShell,
			expected:  []string{"sh", "-i", "-c", "env"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ProbeCommand(tt.probeType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseProbeType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected ProbeType
	}{
		{
			name:     "loginShell",
			input:    "loginShell",
			expected: ProbeLoginShell,
		},
		{
			name:     "loginInteractiveShell",
			input:    "loginInteractiveShell",
			expected: ProbeLoginInteractiveShell,
		},
		{
			name:     "interactiveShell",
			input:    "interactiveShell",
			expected: ProbeInteractiveShell,
		},
		{
			name:     "none",
			input:    "none",
			expected: ProbeNone,
		},
		{
			name:     "empty",
			input:    "",
			expected: ProbeNone,
		},
		{
			name:     "invalid defaults to none",
			input:    "invalid",
			expected: ProbeNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseProbeType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
