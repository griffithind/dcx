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
			result := ParseMount(tt.input)
			require.NotNil(t, result)
			assert.Equal(t, tt.expected.Source, result.Source)
			assert.Equal(t, tt.expected.Target, result.Target)
			assert.Equal(t, tt.expected.ReadOnly, result.ReadOnly)
		})
	}
}

func TestMount_ToDockerFormat(t *testing.T) {
	tests := []struct {
		name     string
		mount    *Mount
		expected string
	}{
		{
			name: "basic mount",
			mount: &Mount{
				Source: "/host",
				Target: "/container",
				Type:   "bind",
			},
			expected: "/host:/container",
		},
		{
			name: "readonly mount",
			mount: &Mount{
				Source:   "/host",
				Target:   "/container",
				Type:     "bind",
				ReadOnly: true,
			},
			expected: "/host:/container:ro",
		},
		{
			name: "with consistency",
			mount: &Mount{
				Source:      "/host",
				Target:      "/container",
				Type:        "bind",
				Consistency: "cached",
			},
			expected: "/host:/container:cached",
		},
		{
			name: "readonly with consistency",
			mount: &Mount{
				Source:      "/host",
				Target:      "/container",
				Type:        "bind",
				ReadOnly:    true,
				Consistency: "delegated",
			},
			expected: "/host:/container:ro,delegated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mount.ToDockerFormat()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMount_ToDockerFormatWithSuffix(t *testing.T) {
	m := &Mount{
		Source: "/host",
		Target: "/container",
		Type:   "bind",
	}

	assert.Equal(t, "/host:/container:Z", m.ToDockerFormatWithSuffix(":Z"))

	m.ReadOnly = true
	assert.Equal(t, "/host:/container:ro,Z", m.ToDockerFormatWithSuffix(":Z"))

	m.Consistency = "cached"
	assert.Equal(t, "/host:/container:ro,cached,Z", m.ToDockerFormatWithSuffix(":Z"))

	m.ReadOnly = false
	assert.Equal(t, "/host:/container:cached,Z", m.ToDockerFormatWithSuffix(":Z"))
}

func TestMount_ToComposeFormat(t *testing.T) {
	tests := []struct {
		name     string
		mount    *Mount
		suffix   string
		expected string
	}{
		{
			name: "bind mount",
			mount: &Mount{
				Source: "/host",
				Target: "/container",
				Type:   "bind",
			},
			suffix:   "",
			expected: "/host:/container",
		},
		{
			name: "volume mount",
			mount: &Mount{
				Source: "myvolume",
				Target: "/data",
				Type:   "volume",
			},
			suffix:   "",
			expected: "myvolume:/data",
		},
		{
			name: "tmpfs mount",
			mount: &Mount{
				Target: "/tmp/cache",
				Type:   "tmpfs",
			},
			suffix:   "",
			expected: "tmpfs:/tmp/cache",
		},
		{
			name: "bind with SELinux",
			mount: &Mount{
				Source: "/host",
				Target: "/container",
				Type:   "bind",
			},
			suffix:   ":Z",
			expected: "/host:/container:Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mount.ToComposeFormat(tt.suffix)
			assert.Equal(t, tt.expected, result)
		})
	}
}
