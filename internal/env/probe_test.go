package env

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProbeCommand(t *testing.T) {
	tests := []struct {
		name        string
		probeType   ProbeType
		expectedCmd []string
	}{
		{
			name:        "none",
			probeType:   ProbeNone,
			expectedCmd: nil,
		},
		{
			name:        "loginShell",
			probeType:   ProbeLoginShell,
			expectedCmd: []string{"sh", "-l", "-c", "env"},
		},
		{
			name:        "loginInteractiveShell",
			probeType:   ProbeLoginInteractiveShell,
			expectedCmd: []string{"sh", "-l", "-i", "-c", "env"},
		},
		{
			name:        "interactiveShell",
			probeType:   ProbeInteractiveShell,
			expectedCmd: []string{"sh", "-i", "-c", "env"},
		},
		{
			name:        "empty",
			probeType:   ProbeType(""),
			expectedCmd: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := ProbeCommand(tt.probeType)
			assert.Equal(t, tt.expectedCmd, cmd)
		})
	}
}

func TestParseProbeType(t *testing.T) {
	tests := []struct {
		input    string
		expected ProbeType
	}{
		{"none", ProbeNone},
		{"", ProbeNone},
		{"loginShell", ProbeLoginShell},
		{"loginInteractiveShell", ProbeLoginInteractiveShell},
		{"interactiveShell", ProbeInteractiveShell},
		{"invalid", ProbeNone},
		{"LOGINSHELL", ProbeNone}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseProbeType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseEnvOutput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:  "simple env vars",
			input: "FOO=bar\nBAZ=qux\n",
			expected: map[string]string{
				"FOO": "bar",
				"BAZ": "qux",
			},
		},
		{
			name:  "value with equals sign",
			input: "PATH=/usr/bin:/bin\nOPTS=--foo=bar\n",
			expected: map[string]string{
				"PATH": "/usr/bin:/bin",
				"OPTS": "--foo=bar",
			},
		},
		{
			name:  "empty value",
			input: "EMPTY=\nFOO=bar\n",
			expected: map[string]string{
				"EMPTY": "",
				"FOO":   "bar",
			},
		},
		{
			name:     "empty input",
			input:    "",
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseEnvOutput(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
