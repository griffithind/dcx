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
