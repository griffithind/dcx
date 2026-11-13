package devcontainer

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestStringOrSlice_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected StringOrSlice
	}{
		{
			name:     "single string",
			input:    `"docker-compose.yml"`,
			expected: StringOrSlice{"docker-compose.yml"},
		},
		{
			name:     "array of strings",
			input:    `["docker-compose.yml", "docker-compose.override.yml"]`,
			expected: StringOrSlice{"docker-compose.yml", "docker-compose.override.yml"},
		},
		{
			name:     "empty array",
			input:    `[]`,
			expected: StringOrSlice{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result StringOrSlice
			if err := json.Unmarshal([]byte(tt.input), &result); err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("got %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestStringOrSlice_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    StringOrSlice
		expected string
	}{
		{
			name:     "single string",
			input:    StringOrSlice{"docker-compose.yml"},
			expected: `"docker-compose.yml"`,
		},
		{
			name:     "multiple strings",
			input:    StringOrSlice{"a.yml", "b.yml"},
			expected: `["a.yml","b.yml"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := json.Marshal(tt.input)
			if err != nil {
				t.Fatalf("MarshalJSON failed: %v", err)
			}
			if string(result) != tt.expected {
				t.Errorf("got %s, expected %s", string(result), tt.expected)
			}
		})
	}
}

func TestLifecycleCommand_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantCommands int
		wantCmd      string
		wantArgs     []string
	}{
		{
			name:         "string command",
			input:        `"echo hello"`,
			wantCommands: 1,
			wantCmd:      "echo hello",
		},
		{
			name:         "array command",
			input:        `["echo", "hello"]`,
			wantCommands: 1,
			wantArgs:     []string{"echo", "hello"},
		},
		{
			name:         "map commands",
			input:        `{"setup": "npm install", "build": ["npm", "run", "build"]}`,
			wantCommands: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result LifecycleCommand
			if err := json.Unmarshal([]byte(tt.input), &result); err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}
			if len(result.Commands) != tt.wantCommands {
				t.Errorf("got %d commands, expected %d", len(result.Commands), tt.wantCommands)
			}
			if tt.wantCmd != "" && result.Commands[0].Command != tt.wantCmd {
				t.Errorf("got command %q, expected %q", result.Commands[0].Command, tt.wantCmd)
			}
			if tt.wantArgs != nil && !reflect.DeepEqual(result.Commands[0].Args, tt.wantArgs) {
				t.Errorf("got args %v, expected %v", result.Commands[0].Args, tt.wantArgs)
			}
		})
	}
}

func TestLifecycleCommand_GetCommands(t *testing.T) {
	cmd := &LifecycleCommand{
		Commands: []LifecycleEntry{
			{Command: "echo hello"},
			{Args: []string{"npm", "install"}},
		},
	}
	cmds := cmd.GetCommands()
	if len(cmds) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cmds))
	}
	if cmds[0] != "echo hello" {
		t.Errorf("expected 'echo hello', got %q", cmds[0])
	}
	if cmds[1] != "npm install" {
		t.Errorf("expected 'npm install', got %q", cmds[1])
	}
}
