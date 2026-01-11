package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMount_DevcontainerFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *ParsedMount
	}{
		{
			name:  "basic bind mount",
			input: "source=/host/path,target=/container/path,type=bind",
			expected: &ParsedMount{
				Source: "/host/path",
				Target: "/container/path",
				Type:   "bind",
			},
		},
		{
			name:  "bind mount with readonly",
			input: "source=/host/path,target=/container/path,type=bind,readonly=true",
			expected: &ParsedMount{
				Source:   "/host/path",
				Target:   "/container/path",
				Type:     "bind",
				ReadOnly: true,
			},
		},
		{
			name:  "volume mount",
			input: "source=myvolume,target=/data,type=volume",
			expected: &ParsedMount{
				Source: "myvolume",
				Target: "/data",
				Type:   "volume",
			},
		},
		{
			name:  "using src/dst aliases",
			input: "src=/host,dst=/container",
			expected: &ParsedMount{
				Source: "/host",
				Target: "/container",
				Type:   "bind",
			},
		},
		{
			name:  "with consistency option",
			input: "source=/host,target=/container,type=bind,consistency=cached",
			expected: &ParsedMount{
				Source:      "/host",
				Target:      "/container",
				Type:        "bind",
				Consistency: "cached",
			},
		},
		{
			name:  "tmpfs mount",
			input: "target=/tmp/cache,type=tmpfs",
			expected: &ParsedMount{
				Target: "/tmp/cache",
				Type:   "tmpfs",
			},
		},
		{
			name:  "with standalone readonly",
			input: "source=/host,target=/container,readonly",
			expected: &ParsedMount{
				Source:   "/host",
				Target:   "/container",
				Type:     "bind",
				ReadOnly: true,
			},
		},
		{
			name:  "with standalone ro",
			input: "source=/host,target=/container,ro",
			expected: &ParsedMount{
				Source:   "/host",
				Target:   "/container",
				Type:     "bind",
				ReadOnly: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseMount(tt.input)
			require.NotNil(t, result)
			assert.Equal(t, tt.expected.Source, result.Source)
			assert.Equal(t, tt.expected.Target, result.Target)
			assert.Equal(t, tt.expected.Type, result.Type)
			assert.Equal(t, tt.expected.ReadOnly, result.ReadOnly)
			assert.Equal(t, tt.expected.Consistency, result.Consistency)
		})
	}
}

func TestParseMount_DockerShortFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *ParsedMount
	}{
		{
			name:  "basic short format",
			input: "/host/path:/container/path",
			expected: &ParsedMount{
				Source: "/host/path",
				Target: "/container/path",
				Type:   "bind",
			},
		},
		{
			name:  "with readonly",
			input: "/host/path:/container/path:ro",
			expected: &ParsedMount{
				Source:   "/host/path",
				Target:   "/container/path",
				Type:     "bind",
				ReadOnly: true,
			},
		},
		{
			name:  "named volume",
			input: "myvolume:/data",
			expected: &ParsedMount{
				Source: "myvolume",
				Target: "/data",
				Type:   "bind",
			},
		},
		{
			name:  "with consistency",
			input: "/host/path:/container/path:cached",
			expected: &ParsedMount{
				Source:      "/host/path",
				Target:      "/container/path",
				Type:        "bind",
				Consistency: "cached",
			},
		},
		{
			name:  "with readonly and consistency",
			input: "/host/path:/container/path:ro,delegated",
			expected: &ParsedMount{
				Source:      "/host/path",
				Target:      "/container/path",
				Type:        "bind",
				ReadOnly:    true,
				Consistency: "delegated",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseMount(tt.input)
			require.NotNil(t, result)
			assert.Equal(t, tt.expected.Source, result.Source)
			assert.Equal(t, tt.expected.Target, result.Target)
			assert.Equal(t, tt.expected.ReadOnly, result.ReadOnly)
		})
	}
}
