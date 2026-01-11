package secrets

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
)

func TestFetchSecrets(t *testing.T) {
	tests := []struct {
		name    string
		configs map[string]devcontainer.SecretConfig
		wantErr bool
		wantLen int
	}{
		{
			name:    "empty configs",
			configs: nil,
			wantErr: false,
			wantLen: 0,
		},
		{
			name: "single secret with echo",
			configs: map[string]devcontainer.SecretConfig{
				"TEST_SECRET": "echo secret_value",
			},
			wantErr: false,
			wantLen: 1,
		},
		{
			name: "multiple secrets",
			configs: map[string]devcontainer.SecretConfig{
				"SECRET1": "echo value1",
				"SECRET2": "echo value2",
			},
			wantErr: false,
			wantLen: 2,
		},
		{
			name: "command fails",
			configs: map[string]devcontainer.SecretConfig{
				"FAIL": "exit 1",
			},
			wantErr: true,
		},
		{
			name: "command not found",
			configs: map[string]devcontainer.SecretConfig{
				"NOTFOUND": "nonexistent_command_xyz",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher := NewFetcher(nil)
			secrets, err := fetcher.FetchSecrets(context.Background(), tt.configs)

			if (err != nil) != tt.wantErr {
				t.Errorf("FetchSecrets() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(secrets) != tt.wantLen {
				t.Errorf("FetchSecrets() returned %d secrets, want %d", len(secrets), tt.wantLen)
			}
		})
	}
}

func TestFetchSecrets_Value(t *testing.T) {
	fetcher := NewFetcher(nil)
	configs := map[string]devcontainer.SecretConfig{
		// Use printf which is more portable than echo -n
		"TEST": "printf 'hello_world'",
	}

	secrets, err := fetcher.FetchSecrets(context.Background(), configs)
	if err != nil {
		t.Fatalf("FetchSecrets() error = %v", err)
	}

	if len(secrets) != 1 {
		t.Fatalf("expected 1 secret, got %d", len(secrets))
	}

	if secrets[0].Name != "TEST" {
		t.Errorf("expected name 'TEST', got '%s'", secrets[0].Name)
	}

	if string(secrets[0].Value) != "hello_world" {
		t.Errorf("expected value 'hello_world', got '%s'", string(secrets[0].Value))
	}
}

func TestFetchSecrets_TrimsNewline(t *testing.T) {
	fetcher := NewFetcher(nil)
	configs := map[string]devcontainer.SecretConfig{
		"TEST": "echo value_with_newline",
	}

	secrets, err := fetcher.FetchSecrets(context.Background(), configs)
	if err != nil {
		t.Fatalf("FetchSecrets() error = %v", err)
	}

	// echo adds a newline, which should be trimmed
	if string(secrets[0].Value) != "value_with_newline" {
		t.Errorf("expected value without trailing newline, got '%s'", string(secrets[0].Value))
	}
}

func TestWriteToTempFiles(t *testing.T) {
	secrets := []Secret{
		{Name: "SECRET1", Value: []byte("value1")},
		{Name: "SECRET2", Value: []byte("value2")},
	}

	paths, cleanup, err := WriteToTempFiles(secrets, "test")
	if err != nil {
		t.Fatalf("WriteToTempFiles() error = %v", err)
	}
	defer cleanup()

	if len(paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(paths))
	}

	// Verify files exist and have correct content
	for _, s := range secrets {
		path, ok := paths[s.Name]
		if !ok {
			t.Errorf("missing path for secret %s", s.Name)
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("failed to read temp file for %s: %v", s.Name, err)
			continue
		}

		if !bytes.Equal(content, s.Value) {
			t.Errorf("secret %s: expected value '%s', got '%s'", s.Name, s.Value, content)
		}

		// Verify file permissions
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("failed to stat temp file for %s: %v", s.Name, err)
			continue
		}

		if info.Mode().Perm() != 0600 {
			t.Errorf("secret %s: expected permissions 0600, got %o", s.Name, info.Mode().Perm())
		}
	}

	// Cleanup and verify files are removed
	cleanup()
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("temp file %s still exists after cleanup", path)
		}
	}
}

func TestWriteToTempFiles_Empty(t *testing.T) {
	paths, cleanup, err := WriteToTempFiles(nil, "test")
	if err != nil {
		t.Fatalf("WriteToTempFiles() error = %v", err)
	}

	if len(paths) != 0 {
		t.Errorf("expected 0 paths for empty secrets, got %d", len(paths))
	}

	// Cleanup should be a no-op
	cleanup()
}
