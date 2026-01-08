package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMountUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Mount
		wantErr  bool
	}{
		{
			name:  "string format with all parts",
			input: `"type=bind,source=/host/path,target=/container/path,readonly"`,
			expected: Mount{
				Source:   "/host/path",
				Target:   "/container/path",
				Type:     "bind",
				ReadOnly: true,
				Raw:      "type=bind,source=/host/path,target=/container/path,readonly",
			},
		},
		{
			name:  "string format with src and dst aliases",
			input: `"type=volume,src=/data,dst=/mnt/data"`,
			expected: Mount{
				Source: "/data",
				Target: "/mnt/data",
				Type:   "volume",
				Raw:    "type=volume,src=/data,dst=/mnt/data",
			},
		},
		{
			name:  "string format with destination alias",
			input: `"source=/src,destination=/dest"`,
			expected: Mount{
				Source: "/src",
				Target: "/dest",
				Raw:    "source=/src,destination=/dest",
			},
		},
		{
			name:  "string format with ro shorthand",
			input: `"source=/src,target=/dest,ro"`,
			expected: Mount{
				Source:   "/src",
				Target:   "/dest",
				ReadOnly: true,
				Raw:      "source=/src,target=/dest,ro",
			},
		},
		{
			name:  "string format readonly=true",
			input: `"source=/src,target=/dest,readonly=true"`,
			expected: Mount{
				Source:   "/src",
				Target:   "/dest",
				ReadOnly: true,
				Raw:      "source=/src,target=/dest,readonly=true",
			},
		},
		{
			name:  "object format basic",
			input: `{"source": "/host", "target": "/container", "type": "bind"}`,
			expected: Mount{
				Source: "/host",
				Target: "/container",
				Type:   "bind",
			},
		},
		{
			name:  "object format with readonly",
			input: `{"source": "/data", "target": "/mnt", "type": "volume", "readonly": true}`,
			expected: Mount{
				Source:   "/data",
				Target:   "/mnt",
				Type:     "volume",
				ReadOnly: true,
			},
		},
		{
			name:  "object format minimal",
			input: `{"source": "/src", "target": "/dst"}`,
			expected: Mount{
				Source: "/src",
				Target: "/dst",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Mount
			err := json.Unmarshal([]byte(tt.input), &m)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, m)
		})
	}
}

func TestMountString(t *testing.T) {
	tests := []struct {
		name     string
		mount    Mount
		expected string
	}{
		{
			name: "with raw string preserved",
			mount: Mount{
				Source: "/host",
				Target: "/container",
				Type:   "bind",
				Raw:    "type=bind,source=/host,target=/container",
			},
			expected: "type=bind,source=/host,target=/container",
		},
		{
			name: "without raw generates docker format",
			mount: Mount{
				Source: "/host",
				Target: "/container",
				Type:   "bind",
			},
			expected: "type=bind,source=/host,target=/container",
		},
		{
			name: "defaults type to bind",
			mount: Mount{
				Source: "/host",
				Target: "/container",
			},
			expected: "type=bind,source=/host,target=/container",
		},
		{
			name: "with readonly",
			mount: Mount{
				Source:   "/host",
				Target:   "/container",
				Type:     "bind",
				ReadOnly: true,
			},
			expected: "type=bind,source=/host,target=/container,readonly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.mount.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDevcontainerConfigMountsArray(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Mount
	}{
		{
			name: "array of string mounts",
			input: `{
				"mounts": [
					"type=bind,source=/host1,target=/container1",
					"source=/host2,target=/container2,readonly"
				]
			}`,
			expected: []Mount{
				{
					Source: "/host1",
					Target: "/container1",
					Type:   "bind",
					Raw:    "type=bind,source=/host1,target=/container1",
				},
				{
					Source:   "/host2",
					Target:   "/container2",
					ReadOnly: true,
					Raw:      "source=/host2,target=/container2,readonly",
				},
			},
		},
		{
			name: "array of object mounts",
			input: `{
				"mounts": [
					{"source": "/host1", "target": "/container1", "type": "bind"},
					{"source": "/host2", "target": "/container2", "readonly": true}
				]
			}`,
			expected: []Mount{
				{
					Source: "/host1",
					Target: "/container1",
					Type:   "bind",
				},
				{
					Source:   "/host2",
					Target:   "/container2",
					ReadOnly: true,
				},
			},
		},
		{
			name: "mixed string and object mounts",
			input: `{
				"mounts": [
					"type=bind,source=/string,target=/path",
					{"source": "/object", "target": "/path", "type": "volume"}
				]
			}`,
			expected: []Mount{
				{
					Source: "/string",
					Target: "/path",
					Type:   "bind",
					Raw:    "type=bind,source=/string,target=/path",
				},
				{
					Source: "/object",
					Target: "/path",
					Type:   "volume",
				},
			},
		},
		{
			name:     "empty mounts array",
			input:    `{"mounts": []}`,
			expected: []Mount{},
		},
		{
			name:     "no mounts field",
			input:    `{"name": "test"}`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg DevcontainerConfig
			err := json.Unmarshal([]byte(tt.input), &cfg)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.Mounts)
		})
	}
}

func TestGetAppPorts(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "single integer port",
			input:    `{"appPort": 3000}`,
			expected: []string{"3000:3000"},
		},
		{
			name:     "single string port",
			input:    `{"appPort": "8080:80"}`,
			expected: []string{"8080:80"},
		},
		{
			name:     "array of integer ports",
			input:    `{"appPort": [3000, 5000]}`,
			expected: []string{"3000:3000", "5000:5000"},
		},
		{
			name:     "array of mixed ports",
			input:    `{"appPort": [3000, "8080:80"]}`,
			expected: []string{"3000:3000", "8080:80"},
		},
		{
			name:     "no appPort",
			input:    `{"name": "test"}`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg DevcontainerConfig
			err := json.Unmarshal([]byte(tt.input), &cfg)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.GetAppPorts())
		})
	}
}

func TestGetForwardPorts(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{
			name:     "array of integer ports",
			input:    `{"forwardPorts": [3000, 5000]}`,
			expected: []string{"3000:3000", "5000:5000"},
		},
		{
			name:     "array of string ports",
			input:    `{"forwardPorts": ["3000", "8080:80"]}`,
			expected: []string{"3000", "8080:80"},
		},
		{
			name:     "empty array",
			input:    `{"forwardPorts": []}`,
			expected: nil,
		},
		{
			name:     "no forwardPorts",
			input:    `{"name": "test"}`,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg DevcontainerConfig
			err := json.Unmarshal([]byte(tt.input), &cfg)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.GetForwardPorts())
		})
	}
}

func TestGetPortAttribute(t *testing.T) {
	input := `{
		"portsAttributes": {
			"3000": {
				"label": "Web App",
				"protocol": "http",
				"onAutoForward": "notify",
				"requireLocalPort": true,
				"elevateIfNeeded": true
			}
		}
	}`

	var cfg DevcontainerConfig
	err := json.Unmarshal([]byte(input), &cfg)
	require.NoError(t, err)

	attr := cfg.GetPortAttribute("3000")
	require.NotNil(t, attr)
	assert.Equal(t, "Web App", attr.Label)
	assert.Equal(t, "http", attr.Protocol)
	assert.Equal(t, "notify", attr.OnAutoForward)
	assert.True(t, attr.RequireLocalPort)
	assert.True(t, attr.ElevateIfNeeded)

	// Non-existent port
	assert.Nil(t, cfg.GetPortAttribute("9999"))
}
