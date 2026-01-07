package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShortcutUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Shortcut
		wantErr  bool
	}{
		{
			name:  "simple command string",
			input: `"bin/jobs --skip-recurring"`,
			expected: Shortcut{
				Command: "bin/jobs --skip-recurring",
			},
		},
		{
			name:  "object with command",
			input: `{"command": "rails server -b 0.0.0.0", "description": "Start Rails server"}`,
			expected: Shortcut{
				Command:     "rails server -b 0.0.0.0",
				Description: "Start Rails server",
			},
		},
		{
			name:  "object with prefix and passArgs",
			input: `{"prefix": "rails", "passArgs": true, "description": "Rails command"}`,
			expected: Shortcut{
				Prefix:      "rails",
				PassArgs:    true,
				Description: "Rails command",
			},
		},
		{
			name:  "object with prefix only",
			input: `{"prefix": "bundle exec"}`,
			expected: Shortcut{
				Prefix: "bundle exec",
			},
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Shortcut
			err := json.Unmarshal([]byte(tt.input), &s)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, s)
		})
	}
}

func TestShortcutMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    Shortcut
		expected string
	}{
		{
			name:     "simple command marshals to string",
			input:    Shortcut{Command: "bin/jobs"},
			expected: `"bin/jobs"`,
		},
		{
			name: "command with description marshals to object",
			input: Shortcut{
				Command:     "rails server",
				Description: "Start server",
			},
			expected: `{"command":"rails server","description":"Start server"}`,
		},
		{
			name: "prefix with passArgs marshals to object",
			input: Shortcut{
				Prefix:   "rails",
				PassArgs: true,
			},
			expected: `{"prefix":"rails","passArgs":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.input)
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(data))
		})
	}
}

func TestParseDcxConfig(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(t *testing.T, cfg *DcxConfig)
	}{
		{
			name: "full config",
			input: `{
				"name": "myproject",
				"up": {
					"ssh": true,
					"noAgent": false
				},
				"shortcuts": {
					"rw": "bin/jobs --skip-recurring",
					"r": {"prefix": "rails", "passArgs": true},
					"rs": {"command": "rails server -b 0.0.0.0", "description": "Start server"}
				}
			}`,
			check: func(t *testing.T, cfg *DcxConfig) {
				assert.Equal(t, "myproject", cfg.Name)
				assert.True(t, cfg.Up.SSH)
				assert.False(t, cfg.Up.NoAgent)
				assert.Len(t, cfg.Shortcuts, 3)

				// Check simple command shortcut
				rw := cfg.Shortcuts["rw"]
				assert.Equal(t, "bin/jobs --skip-recurring", rw.Command)

				// Check prefix shortcut
				r := cfg.Shortcuts["r"]
				assert.Equal(t, "rails", r.Prefix)
				assert.True(t, r.PassArgs)

				// Check command with description
				rs := cfg.Shortcuts["rs"]
				assert.Equal(t, "rails server -b 0.0.0.0", rs.Command)
				assert.Equal(t, "Start server", rs.Description)
			},
		},
		{
			name: "name only",
			input: `{
				"name": "simple"
			}`,
			check: func(t *testing.T, cfg *DcxConfig) {
				assert.Equal(t, "simple", cfg.Name)
				assert.False(t, cfg.Up.SSH)
				assert.Nil(t, cfg.Shortcuts)
			},
		},
		{
			name: "shortcuts only",
			input: `{
				"shortcuts": {
					"test": "npm test"
				}
			}`,
			check: func(t *testing.T, cfg *DcxConfig) {
				assert.Empty(t, cfg.Name)
				assert.Len(t, cfg.Shortcuts, 1)
			},
		},
		{
			name: "config with comments",
			input: `{
				// This is a comment
				"name": "test",
				/* Multi-line
				   comment */
				"shortcuts": {
					"hello": "echo hello"
				}
			}`,
			check: func(t *testing.T, cfg *DcxConfig) {
				assert.Equal(t, "test", cfg.Name)
				assert.Len(t, cfg.Shortcuts, 1)
			},
		},
		{
			name: "config with trailing comma",
			input: `{
				"name": "test",
				"shortcuts": {
					"hello": "echo hello",
				},
			}`,
			check: func(t *testing.T, cfg *DcxConfig) {
				assert.Equal(t, "test", cfg.Name)
			},
		},
		{
			name:    "invalid json",
			input:   `{invalid}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory with dcx.json
			tmpDir := t.TempDir()
			devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
			err := os.MkdirAll(devcontainerDir, 0755)
			require.NoError(t, err)

			dcxPath := filepath.Join(devcontainerDir, "dcx.json")
			err = os.WriteFile(dcxPath, []byte(tt.input), 0644)
			require.NoError(t, err)

			cfg, err := LoadDcxConfig(tmpDir)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cfg)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}

func TestLoadDcxConfigMissing(t *testing.T) {
	tmpDir := t.TempDir()

	// No dcx.json exists
	cfg, err := LoadDcxConfig(tmpDir)
	require.NoError(t, err, "missing dcx.json should not be an error")
	assert.Nil(t, cfg, "should return nil when dcx.json doesn't exist")
}

func TestLoadDcxConfigEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create .devcontainer but no dcx.json
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	cfg, err := LoadDcxConfig(tmpDir)
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestDcxConfigUpOptions(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedSSH     bool
		expectedNoAgent bool
	}{
		{
			name:            "ssh enabled",
			input:           `{"up": {"ssh": true}}`,
			expectedSSH:     true,
			expectedNoAgent: false,
		},
		{
			name:            "noAgent enabled",
			input:           `{"up": {"noAgent": true}}`,
			expectedSSH:     false,
			expectedNoAgent: true,
		},
		{
			name:            "both enabled",
			input:           `{"up": {"ssh": true, "noAgent": true}}`,
			expectedSSH:     true,
			expectedNoAgent: true,
		},
		{
			name:            "empty up",
			input:           `{"up": {}}`,
			expectedSSH:     false,
			expectedNoAgent: false,
		},
		{
			name:            "no up",
			input:           `{"name": "test"}`,
			expectedSSH:     false,
			expectedNoAgent: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
			err := os.MkdirAll(devcontainerDir, 0755)
			require.NoError(t, err)

			dcxPath := filepath.Join(devcontainerDir, "dcx.json")
			err = os.WriteFile(dcxPath, []byte(tt.input), 0644)
			require.NoError(t, err)

			cfg, err := LoadDcxConfig(tmpDir)
			require.NoError(t, err)
			require.NotNil(t, cfg)

			assert.Equal(t, tt.expectedSSH, cfg.Up.SSH)
			assert.Equal(t, tt.expectedNoAgent, cfg.Up.NoAgent)
		})
	}
}

func TestOuzoERPStyleDcxConfig(t *testing.T) {
	input := `{
		"name": "ouzoerp",
		"up": {
			"ssh": true
		},
		"shortcuts": {
			"r": {
				"prefix": "rails",
				"passArgs": true,
				"description": "Rails command with arguments"
			},
			"rw": {
				"command": "bin/jobs --skip-recurring",
				"description": "Run workers"
			},
			"rs": {
				"command": "rails server -b 0.0.0.0",
				"description": "Start Rails server"
			},
			"rc": {
				"command": "rails console",
				"description": "Rails console"
			},
			"test": {
				"prefix": "rails test",
				"passArgs": true,
				"description": "Run tests"
			},
			"wds": "bun run build --watch"
		}
	}`

	tmpDir := t.TempDir()
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	dcxPath := filepath.Join(devcontainerDir, "dcx.json")
	err = os.WriteFile(dcxPath, []byte(input), 0644)
	require.NoError(t, err)

	cfg, err := LoadDcxConfig(tmpDir)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Check project name
	assert.Equal(t, "ouzoerp", cfg.Name)

	// Check up options
	assert.True(t, cfg.Up.SSH)
	assert.False(t, cfg.Up.NoAgent)

	// Check shortcuts count
	assert.Len(t, cfg.Shortcuts, 6)

	// Check prefix shortcut
	r := cfg.Shortcuts["r"]
	assert.Equal(t, "rails", r.Prefix)
	assert.True(t, r.PassArgs)
	assert.Equal(t, "Rails command with arguments", r.Description)

	// Check command shortcut
	rw := cfg.Shortcuts["rw"]
	assert.Equal(t, "bin/jobs --skip-recurring", rw.Command)
	assert.Equal(t, "Run workers", rw.Description)

	// Check simple string shortcut
	wds := cfg.Shortcuts["wds"]
	assert.Equal(t, "bun run build --watch", wds.Command)
	assert.Empty(t, wds.Description)
}
