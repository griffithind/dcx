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

func TestPortSpec_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		wantContainer int
		wantHost      int
		wantProtocol  string
	}{
		{
			name:          "integer port",
			input:         `8080`,
			wantContainer: 8080,
			wantHost:      8080,
			wantProtocol:  "tcp",
		},
		{
			name:          "string port",
			input:         `"8080"`,
			wantContainer: 8080,
			wantHost:      8080,
			wantProtocol:  "tcp",
		},
		{
			name:          "host:container format",
			input:         `"3000:8080"`,
			wantContainer: 8080,
			wantHost:      3000,
			wantProtocol:  "tcp",
		},
		{
			name:          "with protocol",
			input:         `"5000/udp"`,
			wantContainer: 5000,
			wantHost:      5000,
			wantProtocol:  "udp",
		},
		{
			name:          "object form",
			input:         `{"containerPort": 8080, "hostPort": 3000, "protocol": "tcp"}`,
			wantContainer: 8080,
			wantHost:      3000,
			wantProtocol:  "tcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result PortSpec
			if err := json.Unmarshal([]byte(tt.input), &result); err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}
			if result.Container != tt.wantContainer {
				t.Errorf("container: got %d, expected %d", result.Container, tt.wantContainer)
			}
			if result.Host != tt.wantHost {
				t.Errorf("host: got %d, expected %d", result.Host, tt.wantHost)
			}
			if result.Protocol != tt.wantProtocol {
				t.Errorf("protocol: got %q, expected %q", result.Protocol, tt.wantProtocol)
			}
		})
	}
}

func TestPortSpec_String(t *testing.T) {
	tests := []struct {
		name     string
		spec     PortSpec
		expected string
	}{
		{
			name:     "same host and container",
			spec:     PortSpec{Container: 8080, Host: 8080, Protocol: "tcp"},
			expected: "8080",
		},
		{
			name:     "different host and container",
			spec:     PortSpec{Container: 8080, Host: 3000, Protocol: "tcp"},
			expected: "3000:8080",
		},
		{
			name:     "with udp protocol",
			spec:     PortSpec{Container: 5000, Host: 5000, Protocol: "udp"},
			expected: "5000/udp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.spec.String()
			if result != tt.expected {
				t.Errorf("got %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestGPUConfig_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantEnabled bool
		wantCount   int
	}{
		{
			name:        "boolean true",
			input:       `true`,
			wantEnabled: true,
			wantCount:   -1,
		},
		{
			name:        "boolean false",
			input:       `false`,
			wantEnabled: false,
			wantCount:   0,
		},
		{
			name:        "string all",
			input:       `"all"`,
			wantEnabled: true,
			wantCount:   -1,
		},
		{
			name:        "integer count",
			input:       `2`,
			wantEnabled: true,
			wantCount:   2,
		},
		{
			name:        "object with count",
			input:       `{"count": 2, "memory": "8gb"}`,
			wantEnabled: true,
			wantCount:   2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result GPUConfig
			if err := json.Unmarshal([]byte(tt.input), &result); err != nil {
				t.Fatalf("UnmarshalJSON failed: %v", err)
			}
			if result.Enabled != tt.wantEnabled {
				t.Errorf("enabled: got %v, expected %v", result.Enabled, tt.wantEnabled)
			}
			if result.Count != tt.wantCount {
				t.Errorf("count: got %d, expected %d", result.Count, tt.wantCount)
			}
		})
	}
}

func TestPortSpecs_UnmarshalJSON(t *testing.T) {
	input := `[8080, "3000:8080", {"containerPort": 5000}]`
	var specs PortSpecs
	if err := json.Unmarshal([]byte(input), &specs); err != nil {
		t.Fatalf("UnmarshalJSON failed: %v", err)
	}
	if len(specs) != 3 {
		t.Fatalf("expected 3 port specs, got %d", len(specs))
	}
	if specs[0].Container != 8080 {
		t.Errorf("first port container: got %d, expected 8080", specs[0].Container)
	}
	if specs[1].Host != 3000 {
		t.Errorf("second port host: got %d, expected 3000", specs[1].Host)
	}
	if specs[2].Container != 5000 {
		t.Errorf("third port container: got %d, expected 5000", specs[2].Container)
	}
}
