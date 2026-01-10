package build

import (
	"bytes"
	"testing"
)

func TestContextBuilder_AddFile(t *testing.T) {
	builder := NewContextBuilder()

	content := []byte("hello world")
	if err := builder.AddFile("test.txt", content, 0644); err != nil {
		t.Fatalf("AddFile failed: %v", err)
	}

	reader, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	// Verify we got some content
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(reader); err != nil {
		t.Fatalf("Failed to read tar: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Expected non-empty tar archive")
	}
}

func TestContextBuilder_MultipleFiles(t *testing.T) {
	builder := NewContextBuilder()

	files := map[string][]byte{
		"file1.txt":     []byte("content1"),
		"dir/file2.txt": []byte("content2"),
		"Dockerfile":    []byte("FROM alpine"),
	}

	for name, content := range files {
		if err := builder.AddFile(name, content, 0644); err != nil {
			t.Fatalf("AddFile(%s) failed: %v", name, err)
		}
	}

	reader, err := builder.Build()
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}

	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(reader); err != nil {
		t.Fatalf("Failed to read tar: %v", err)
	}

	if buf.Len() == 0 {
		t.Error("Expected non-empty tar archive")
	}
}

func TestDefaultExcludePatterns(t *testing.T) {
	patterns := DefaultExcludePatterns()

	if len(patterns) == 0 {
		t.Error("Expected non-empty exclude patterns")
	}

	// Check for expected patterns
	expected := []string{".git", "node_modules", "__pycache__"}
	for _, exp := range expected {
		found := false
		for _, p := range patterns {
			if p == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected pattern %q not found", exp)
		}
	}
}

func TestShouldUpdateRemoteUserUID(t *testing.T) {
	tests := []struct {
		name       string
		remoteUser string
		hostUID    int
		expected   bool
	}{
		{
			name:       "normal user",
			remoteUser: "vscode",
			hostUID:    1000,
			expected:   true,
		},
		{
			name:       "root user",
			remoteUser: "root",
			hostUID:    1000,
			expected:   false,
		},
		{
			name:       "root UID string",
			remoteUser: "0",
			hostUID:    1000,
			expected:   false,
		},
		{
			name:       "host is root",
			remoteUser: "vscode",
			hostUID:    0,
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldUpdateRemoteUserUID(tt.remoteUser, tt.hostUID)
			if result != tt.expected {
				t.Errorf("ShouldUpdateRemoteUserUID(%q, %d) = %v, expected %v",
					tt.remoteUser, tt.hostUID, result, tt.expected)
			}
		})
	}
}

func TestShouldUpdateRemoteUserUIDWithConfig(t *testing.T) {
	boolTrue := true
	boolFalse := false

	tests := []struct {
		name                string
		updateRemoteUserUID *bool
		remoteUser          string
		hostUID             int
		expected            bool
	}{
		{
			name:                "explicitly enabled",
			updateRemoteUserUID: &boolTrue,
			remoteUser:          "vscode",
			hostUID:             1000,
			expected:            true,
		},
		{
			name:                "explicitly disabled",
			updateRemoteUserUID: &boolFalse,
			remoteUser:          "vscode",
			hostUID:             1000,
			expected:            false,
		},
		{
			name:                "default (nil) - should update",
			updateRemoteUserUID: nil,
			remoteUser:          "vscode",
			hostUID:             1000,
			expected:            true,
		},
		{
			name:                "root user - config ignored",
			updateRemoteUserUID: &boolTrue,
			remoteUser:          "root",
			hostUID:             1000,
			expected:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ShouldUpdateRemoteUserUIDWithConfig(tt.updateRemoteUserUID, tt.remoteUser, tt.hostUID)
			if result != tt.expected {
				t.Errorf("ShouldUpdateRemoteUserUIDWithConfig = %v, expected %v", result, tt.expected)
			}
		})
	}
}

