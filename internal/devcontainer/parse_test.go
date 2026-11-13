package devcontainer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMount_DevcontainerFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *Mount
	}{
		{
			name:  "basic bind mount",
			input: "source=/host/path,target=/container/path,type=bind",
			expected: &Mount{
				Source: "/host/path",
				Target: "/container/path",
				Type:   "bind",
			},
		},
		{
			name:  "bind mount with readonly",
			input: "source=/host/path,target=/container/path,type=bind,readonly=true",
			expected: &Mount{
				Source:   "/host/path",
				Target:   "/container/path",
				Type:     "bind",
				ReadOnly: true,
			},
		},
		{
			name:  "volume mount",
			input: "source=myvolume,target=/data,type=volume",
			expected: &Mount{
				Source: "myvolume",
				Target: "/data",
				Type:   "volume",
			},
		},
		{
			name:  "using src/dst aliases",
			input: "src=/host,dst=/container",
			expected: &Mount{
				Source: "/host",
				Target: "/container",
				Type:   "bind",
			},
		},
		{
			name:  "with consistency option",
			input: "source=/host,target=/container,type=bind,consistency=cached",
			expected: &Mount{
				Source:      "/host",
				Target:      "/container",
				Type:        "bind",
				Consistency: "cached",
			},
		},
		{
			name:  "tmpfs mount",
			input: "target=/tmp/cache,type=tmpfs",
			expected: &Mount{
				Target: "/tmp/cache",
				Type:   "tmpfs",
			},
		},
		{
			name:  "with standalone readonly",
			input: "source=/host,target=/container,readonly",
			expected: &Mount{
				Source:   "/host",
				Target:   "/container",
				Type:     "bind",
				ReadOnly: true,
			},
		},
		{
			name:  "with standalone ro",
			input: "source=/host,target=/container,ro",
			expected: &Mount{
				Source:   "/host",
				Target:   "/container",
				Type:     "bind",
				ReadOnly: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMount(tt.input)
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
		expected *Mount
	}{
		{
			name:  "basic short format",
			input: "/host/path:/container/path",
			expected: &Mount{
				Source: "/host/path",
				Target: "/container/path",
				Type:   "bind",
			},
		},
		{
			name:  "with readonly",
			input: "/host/path:/container/path:ro",
			expected: &Mount{
				Source:   "/host/path",
				Target:   "/container/path",
				Type:     "bind",
				ReadOnly: true,
			},
		},
		{
			name:  "named volume",
			input: "myvolume:/data",
			expected: &Mount{
				Source: "myvolume",
				Target: "/data",
				Type:   "bind",
			},
		},
		{
			name:  "with consistency",
			input: "/host/path:/container/path:cached",
			expected: &Mount{
				Source:      "/host/path",
				Target:      "/container/path",
				Type:        "bind",
				Consistency: "cached",
			},
		},
		{
			name:  "with readonly and consistency",
			input: "/host/path:/container/path:ro,delegated",
			expected: &Mount{
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
			result := parseMount(tt.input)
			require.NotNil(t, result)
			assert.Equal(t, tt.expected.Source, result.Source)
			assert.Equal(t, tt.expected.Target, result.Target)
			assert.Equal(t, tt.expected.ReadOnly, result.ReadOnly)
		})
	}
}

func TestParseMount_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "missing target",
			input: "source=/host",
		},
		{
			name:  "missing source for bind",
			input: "target=/container,type=bind",
		},
		{
			name:  "short format missing target",
			input: "/host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMount(tt.input)
			assert.Nil(t, result)
		})
	}
}

// parseMount adapts ParseMountString for the table tests above.
func parseMount(s string) *Mount {
	m, err := ParseMountString(s)
	if err != nil {
		return nil
	}
	return &m
}

// A value containing ':' after substitution (e.g. ${localEnv:HOME}) must
// not be mistaken for the short "src:dst" form.
func TestParseMountStringKeyValueWithColons(t *testing.T) {
	m, err := ParseMountString("type=bind,src=/home/u/c:d/.ssh,dst=/root/.ssh,readonly")
	require.NoError(t, err)
	assert.Equal(t, Mount{Source: "/home/u/c:d/.ssh", Target: "/root/.ssh", Type: "bind", ReadOnly: true}, m)
}

func TestParseMountStringShortNamedVolume(t *testing.T) {
	m, err := ParseMountString("cache:/cache")
	require.NoError(t, err)
	assert.Equal(t, "volume", m.Type)
}

func TestParseMountStringErrors(t *testing.T) {
	for _, s := range []string{"", "target=", "source=/a", "nocolon", ":/x"} {
		_, err := ParseMountString(s)
		assert.Error(t, err, s)
	}
}
