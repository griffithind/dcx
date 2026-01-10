package common

import "testing"

func TestSanitizeProjectName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "simple lowercase",
			input:    "myproject",
			expected: "myproject",
		},
		{
			name:     "uppercase",
			input:    "MyProject",
			expected: "myproject",
		},
		{
			name:     "with spaces",
			input:    "my project",
			expected: "my_project",
		},
		{
			name:     "with hyphens",
			input:    "my-project",
			expected: "my-project",
		},
		{
			name:     "with underscores",
			input:    "my_project",
			expected: "my_project",
		},
		{
			name:     "starts with number",
			input:    "123project",
			expected: "dcx_123project",
		},
		{
			name:     "special characters",
			input:    "my@project!name",
			expected: "myprojectname",
		},
		{
			name:     "mixed case and special",
			input:    "My Project (Test)",
			expected: "my_project_test",
		},
		{
			name:     "only special characters",
			input:    "@#$%",
			expected: "",
		},
		{
			name:     "unicode characters",
			input:    "projet-test",
			expected: "projet-test",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeProjectName(tt.input)
			if result != tt.expected {
				t.Errorf("SanitizeProjectName(%q) = %q, expected %q", tt.input, result, tt.expected)
			}
		})
	}
}
