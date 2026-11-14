package features

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractDigestFromResolved(t *testing.T) {
	tests := []struct {
		name     string
		resolved string
		expected string
	}{
		{
			name:     "OCI with sha256 digest",
			resolved: "ghcr.io/devcontainers/features/common-utils@sha256:abc123def456",
			expected: "sha256:abc123def456",
		},
		{
			name:     "OCI with full digest",
			resolved: "ghcr.io/devcontainers/features/go@sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
			expected: "sha256:1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		},
		{
			name:     "OCI without digest (tag reference)",
			resolved: "ghcr.io/devcontainers/features/common-utils:1.0.0",
			expected: "",
		},
		{
			name:     "HTTP URL (no digest)",
			resolved: "https://example.com/feature.tgz",
			expected: "",
		},
		{
			name:     "empty string",
			resolved: "",
			expected: "",
		},
		{
			name:     "sha384 digest",
			resolved: "registry.io/repo/feature@sha384:abc123",
			expected: "sha384:abc123",
		},
		{
			name:     "sha512 digest",
			resolved: "registry.io/repo/feature@sha512:abc123",
			expected: "sha512:abc123",
		},
		{
			name:     "invalid digest format",
			resolved: "registry.io/repo/feature@invalid:abc123",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDigestFromResolved(tt.resolved)
			if result != tt.expected {
				t.Errorf("extractDigestFromResolved(%q) = %q, want %q", tt.resolved, result, tt.expected)
			}
		})
	}
}

// devcontainer-feature.json is JSONC, as the reference implementation
// reads it (compat F13).
func TestLoadMetadataAcceptsJSONC(t *testing.T) {
	dir := t.TempDir()
	data := "\xEF\xBB\xBF{\n  // a comment\n  \"id\": \"hello\",\n  \"version\": \"1.0.0\",\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "devcontainer-feature.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	md, err := (&Resolver{}).loadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if md.ID != "hello" || md.Version != "1.0.0" {
		t.Errorf("got %+v", md)
	}
}
